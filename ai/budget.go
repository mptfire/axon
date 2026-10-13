package ai

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Budget tracks daily token usage (input+output) per visitor and globally, and enforces
// the configured daily budgets. Usage is kept in memory and resets at midnight UTC and
// on server restart. See UsageReport for what is exposed to the API.
type Budget struct {
	visitorDaily int64 // Max tokens per visitor per day; 0 = unlimited
	globalDaily  int64 // Max tokens server-wide per day; 0 = unlimited

	mu              sync.Mutex
	day             string // UTC day the counters belong to, e.g. "2026-09-24"
	global          Usage  // Global usage for today
	reserved        int64
	visitorReserved map[string]int64
	perVisitor      map[string]*Usage // Per-visitor usage for today
	now             func() time.Time  // Injectable clock for tests
}

// Usage is a token/request counter for one day.
type Usage struct {
	Requests     int64 `json:"requests"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Total returns input+output tokens.
func (u *Usage) Total() int64 { return saturatingAdd(u.InputTokens, u.OutputTokens) }

// UsageReport is the API-facing view of today's usage.
type UsageReport struct {
	Day           string `json:"day"`
	Global        Usage  `json:"global"`
	Visitor       Usage  `json:"visitor"`
	VisitorBudget int64  `json:"visitor_daily_budget"` // 0 = unlimited
	GlobalBudget  int64  `json:"global_daily_budget"`  // 0 = unlimited
}

// NewBudget creates a Budget. Budget values of 0 mean unlimited.
func NewBudget(visitorDaily, globalDaily int64) *Budget {
	return &Budget{
		visitorDaily:    visitorDaily,
		globalDaily:     globalDaily,
		perVisitor:      make(map[string]*Usage),
		visitorReserved: make(map[string]int64),
		now:             time.Now,
	}
}

// Allow returns ErrBudgetExceeded if the visitor or the global daily budget is already
// exhausted. It does not reserve tokens; Record charges actual usage afterwards, so a
// callers performing provider work use Reserve to include in-flight cost. Actual
// provider usage can exceed the input-token estimate; subsequent calls are blocked.
func (b *Budget) Allow(userKey string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	if b.globalDaily > 0 && b.global.Total() >= b.globalDaily {
		return fmt.Errorf("%w: global daily budget of %d tokens reached", ErrBudgetExceeded, b.globalDaily)
	}
	if userKey != "" && b.visitorDaily > 0 {
		if u := b.perVisitor[userKey]; u != nil && u.Total() >= b.visitorDaily {
			return fmt.Errorf("%w: visitor daily budget of %d tokens reached", ErrBudgetExceeded, b.visitorDaily)
		}
	}
	return nil
}

// Record charges completed usage to the visitor and the global counter.
func (b *Budget) Record(userKey string, input, output int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	b.record(userKey, input, output)
}

func (b *Budget) record(userKey string, input, output int64) {
	b.global.Requests = saturatingAdd(b.global.Requests, 1)
	b.global.InputTokens = saturatingAdd(b.global.InputTokens, input)
	b.global.OutputTokens = saturatingAdd(b.global.OutputTokens, output)
	if userKey == "" {
		return
	}
	u, ok := b.perVisitor[userKey]
	if !ok {
		u = &Usage{}
		b.perVisitor[userKey] = u
	}
	u.Requests = saturatingAdd(u.Requests, 1)
	u.InputTokens = saturatingAdd(u.InputTokens, input)
	u.OutputTokens = saturatingAdd(u.OutputTokens, output)
}

// Report returns today's usage for a visitor key and the configured budgets.
func (b *Budget) Report(userKey string) *UsageReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	report := &UsageReport{
		Day:           b.day,
		Global:        b.global,
		VisitorBudget: b.visitorDaily,
		GlobalBudget:  b.globalDaily,
	}
	if userKey != "" {
		if u, ok := b.perVisitor[userKey]; ok {
			report.Visitor = *u
		}
	}
	return report
}

// rollover resets the counters when the UTC day changed. Must be called with b.mu held.
func (b *Budget) rollover() {
	today := b.now().UTC().Format("2006-01-02")
	if b.day == today {
		return
	}
	b.day = today
	b.global = Usage{}
	b.perVisitor = make(map[string]*Usage)
}

// Reservation holds an estimated input + maximum output charge while a provider
// call is in flight. Finish releases it and records actual (or estimated) usage.
type Reservation struct {
	budget *Budget
	key    string
	tokens int64
	done   bool
}

// Reserve checks completed and in-flight usage atomically. It never calls a provider.
func (b *Budget) Reserve(key string, tokens int64) (*Reservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	tokens = max(0, tokens)
	if b.globalDaily > 0 && (b.global.Total() >= b.globalDaily || b.reserved > b.globalDaily-b.global.Total() || tokens > b.globalDaily-b.global.Total()-b.reserved) {
		return nil, fmt.Errorf("%w: global daily budget", ErrBudgetExceeded)
	}
	if key != "" && b.visitorDaily > 0 {
		used := int64(0)
		if u := b.perVisitor[key]; u != nil {
			used = u.Total()
		}
		pending := b.visitorReserved[key]
		if used >= b.visitorDaily || pending > b.visitorDaily-used || tokens > b.visitorDaily-used-pending {
			return nil, fmt.Errorf("%w: visitor daily budget", ErrBudgetExceeded)
		}
	}
	b.reserved = saturatingAdd(b.reserved, tokens)
	b.visitorReserved[key] = saturatingAdd(b.visitorReserved[key], tokens)
	return &Reservation{budget: b, key: key, tokens: tokens}, nil
}

// Finish releases a reservation once, including when the UTC day rolled over.
func (r *Reservation) Finish(input, output int64) {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	b.rollover()
	b.reserved -= r.tokens
	b.visitorReserved[r.key] -= r.tokens
	if b.visitorReserved[r.key] == 0 {
		delete(b.visitorReserved, r.key)
	}
	b.record(r.key, input, output)
}

func saturatingAdd(a, b int64) int64 {
	a, b = max(0, a), max(0, b)
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
