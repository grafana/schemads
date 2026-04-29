package cache

import (
	"context"
	"reflect"
	"time"
)

// Typed is an in-process, type-safe cache for hot paths where JSON
// marshal/unmarshal would dominate CPU.
//
// Used by plugins for in-handler sub-fetches (e.g. Azure workspaces, ES field
// caps) where the cached value is a Go struct that's never serialised over
// the wire. Concurrent misses for the same key are deduped via the owning
// [MemoryCache]; errors are never cached.
//
// Scope is controlled by the [Key] passed to Get/GetOrFetch. Build that key
// with [KeyFromPluginContext] using either [ScopeUser] or [ScopeDatasource].
type Typed[T any] struct {
	cache    *MemoryCache
	endpoint string
	valueTyp reflect.Type
}

// NewTyped returns a Typed cache. The endpoint label is used for Prometheus
// metrics (typically "subfetch:<name>") and otherwise has no effect.
func NewTyped[T any](cache *MemoryCache, endpoint string) *Typed[T] {
	if endpoint == "" {
		endpoint = "subfetch"
	}
	return &Typed[T]{
		cache:    cache,
		endpoint: endpoint,
		valueTyp: reflect.TypeOf((*T)(nil)).Elem(),
	}
}

// Get returns the cached value for key, or ok=false on a miss or expiry.
func (t *Typed[T]) Get(ctx context.Context, key Key) (T, bool) {
	var zero T
	if t == nil || t.cache == nil {
		recordMiss(endpointForTyped(t))
		return zero, false
	}
	v, ok := t.cache.getTyped(key, t.endpoint, t.valueTyp, true)
	if !ok {
		return zero, false
	}
	out, ok := v.(T)
	if !ok {
		t.cache.deleteTyped(key, t.endpoint)
		recordMiss(t.endpoint)
		return zero, false
	}
	return out, true
}

// Set stores value at key with the given TTL. Zero ttl is a no-op.
func (t *Typed[T]) Set(ctx context.Context, key Key, value T, ttl time.Duration) {
	if t == nil || t.cache == nil || ttl <= 0 {
		return
	}
	t.cache.setTyped(key, t.endpoint, value, t.valueTyp, ttl)
}

// Delete removes the entry for key.
func (t *Typed[T]) Delete(ctx context.Context, key Key) {
	if t == nil || t.cache == nil {
		return
	}
	t.cache.deleteTyped(key, t.endpoint)
}

// GetOrFetch returns the cached value or runs fn, deduped per key. Errors are
// not cached.
func (t *Typed[T]) GetOrFetch(
	ctx context.Context,
	key Key,
	ttl time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	if t == nil || t.cache == nil || ttl <= 0 {
		return fn(ctx)
	}
	return getOrFetchTyped(ctx, t.cache, key, t.endpoint, t.valueTyp, ttl, fn)
}

// Len returns the number of entries in the underlying memory cache. Intended
// for tests.
func (t *Typed[T]) Len() int {
	if t == nil || t.cache == nil {
		return 0
	}
	return t.cache.ItemCount()
}

func endpointForTyped[T any](t *Typed[T]) string {
	if t == nil || t.endpoint == "" {
		return "subfetch"
	}
	return t.endpoint
}
