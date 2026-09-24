package ai

import (
	"fmt"
	"sync"
	"time"
)

// Budget tracks daily token usage (input+output) per visitor and globally, and enforces
// the configured daily budgets. Usage is kept in memory and resets at midnight UTC and
// on server restart. See UsageReport for what is exposed to the API.
type Budget struct {
	visitorDaily int64 // Max tokens per visitor per day; 0 = unlimited
	globalDaily  int64 // Max tokens server-wide per day; 0 = unlimited

	mu         sync.Mutex
	day        string            // UTC day the counters belong to, e.g. "2026-09-24"
	global     Usage             // Global usage for today
	perVisitor map[string]*Usage // Per-visitor usage for today
	now        func() time.Time  // Injectable clock for tests
}

// Usage is a token/request counter for one day.
type Usage struct {
	Requests     int64 `json:"requests"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Total returns input+output tokens.
func (u *Usage) Total() int64 { return u.InputTokens + u.OutputTokens }

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
		visitorDaily: visitorDaily,
		globalDaily:  globalDaily,
		perVisitor:   make(map[string]*Usage),
		now:          time.Now,
	}
}

// Allow returns ErrBudgetExceeded if the visitor or the global daily budget is already
// exhausted. It does not reserve tokens; Record charges actual usage afterwards, so a
// single in-flight request may overshoot its budget. This is deliberate: estimation
// would either throttle legitimate traffic or be gameable.
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
	b.global.Requests++
	b.global.InputTokens += input
	b.global.OutputTokens += output
	if userKey == "" {
		return
	}
	u, ok := b.perVisitor[userKey]
	if !ok {
		u = &Usage{}
		b.perVisitor[userKey] = u
	}
	u.Requests++
	u.InputTokens += input
	u.OutputTokens += output
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
