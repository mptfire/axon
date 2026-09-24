package ai

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
)

// DefaultCacheSize bounds the response cache if Config.CacheSize is unset.
const DefaultCacheSize = 100 * 1024 * 1024 // 100 MB

// responseCacheEntry is a cached provider response.
type responseCacheEntry struct {
	key   string
	resp  *Response
	bytes int64
}

// Cache is an in-memory LRU cache of provider responses, keyed by the semantic content
// of the request (feature, model, prompts, schema — not the attribution key). It turns
// alert storms and repeated plans into a single provider call.
type Cache struct {
	maxBytes int64

	mu      sync.Mutex
	size    int64
	entries map[string]*list.Element // key -> element of order (front = most recent)
	order   *list.List
}

// NewCache creates a Cache bounded by maxBytes; 0 or negative falls back to DefaultCacheSize.
func NewCache(maxBytes int64) *Cache {
	if maxBytes <= 0 {
		maxBytes = DefaultCacheSize
	}
	return &Cache{
		maxBytes: maxBytes,
		entries:  make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get returns the cached response for req, or nil on a miss. The returned response must
// not be mutated by the caller.
func (c *Cache) Get(req *Request) *Response {
	key := c.key(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		entry := el.Value.(*responseCacheEntry)
		resp := *entry.resp
		resp.Cached = true
		return &resp
	}
	return nil
}

// Store caches a response, evicting least-recently-used entries until the size bound holds.
// Responses larger than the whole cache are not stored.
func (c *Cache) Store(req *Request, resp *Response) {
	bytes := responseBytes(resp)
	if bytes > c.maxBytes {
		return
	}
	key := c.key(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.size -= el.Value.(*responseCacheEntry).bytes
		c.order.Remove(el)
		delete(c.entries, key)
	}
	entry := &responseCacheEntry{key: key, resp: resp, bytes: bytes}
	el := c.order.PushFront(entry)
	c.entries[key] = el
	c.size += bytes
	for c.size > c.maxBytes && c.order.Len() > 1 {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		oldEntry := oldest.Value.(*responseCacheEntry)
		delete(c.entries, oldEntry.key)
		c.size -= oldEntry.bytes
	}
}

// key derives a stable cache key from the request's semantic content. The attribution
// key (UserKey) is intentionally excluded so users share cached results.
func (c *Cache) key(req *Request) string {
	seed := struct {
		Feature     Feature
		Model       string
		System      string
		Messages    []Message
		Prompt      string
		MaxTokens   int
		Temperature float64
		JSONSchema  *Schema
	}{
		Feature:     req.Feature,
		Model:       req.Model,
		System:      req.System,
		Messages:    req.Messages,
		Prompt:      req.Prompt,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		JSONSchema:  req.JSONSchema,
	}
	// encoding/json sorts map keys, so this is deterministic for identical requests
	serialized, err := json.Marshal(seed)
	if err != nil {
		return "" // Cannot happen for these types; misses the cache, never panics
	}
	return fmt.Sprintf("%x", sha256.Sum256(serialized))
}

// responseBytes estimates the memory footprint of a cached response.
func responseBytes(resp *Response) int64 {
	return int64(len(resp.Text) + len(resp.Model) + len(resp.FinishReason) + 128)
}
