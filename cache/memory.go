package cache

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

// MemoryOptions tunes the in-process MemoryCache.
type MemoryOptions struct {
	// DefaultTTL is the TTL applied when callers pass a zero ttl to Set.
	// Zero means "use the package default" (5 minutes). Public Set helpers
	// still treat zero ttl as "do not cache" to match EndpointTTLs.
	DefaultTTL time.Duration
	// CleanupInterval controls how often go-cache scans for expired entries.
	// Zero means "use the package default" (5 minutes). Negative disables the
	// cleanup goroutine; entries still expire on access.
	CleanupInterval time.Duration
	// MaxValueBytes bounds byte-oriented response entries. Values larger than
	// this are not cached. Zero uses the package default.
	MaxValueBytes int
}

// DefaultMemoryOptions returns sensible defaults for the SchemaDatasource
// response cache: 5-minute default TTL, 5-minute cleanup interval, and a 5 MiB
// maximum response value.
func DefaultMemoryOptions() MemoryOptions {
	return MemoryOptions{
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		MaxValueBytes:   5 << 20,
	}
}

// MemoryCache is the SDK-owned in-process cache. It is built on
// patrickmn/go-cache for TTL+eviction, and uses singleflight to dedupe
// concurrent misses.
//
// Safe for concurrent use.
type MemoryCache struct {
	store         *gocache.Cache
	sf            singleflight.Group
	maxValueBytes int
}

type entryKind string

const (
	entryKindBytes entryKind = "bytes"
	entryKindTyped entryKind = "typed"
)

type memoryEntry struct {
	kind      entryKind
	endpoint  string
	value     any
	valueType reflect.Type
}

// NewMemory returns a MemoryCache with the given options. Pass a zero-value
// MemoryOptions to use the defaults.
func NewMemory(opts MemoryOptions) *MemoryCache {
	defaults := DefaultMemoryOptions()
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = defaults.DefaultTTL
	}
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = defaults.CleanupInterval
	}
	if opts.CleanupInterval < 0 {
		opts.CleanupInterval = 0
	}
	if opts.MaxValueBytes <= 0 {
		opts.MaxValueBytes = defaults.MaxValueBytes
	}
	c := gocache.New(opts.DefaultTTL, opts.CleanupInterval)
	mc := &MemoryCache{store: c, maxValueBytes: opts.MaxValueBytes}
	c.OnEvicted(func(_ string, v any) {
		if e, ok := v.(memoryEntry); ok {
			recordEviction(e.endpoint)
		}
	})
	return mc
}

// Get returns the cached bytes for key, or ok=false on a miss.
func (m *MemoryCache) Get(_ context.Context, key Key, endpoint string) ([]byte, bool) {
	return m.getBytes(key, endpoint, true)
}

func (m *MemoryCache) getBytes(key Key, endpoint string, record bool) ([]byte, bool) {
	if m == nil {
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	v, ok := m.store.Get(bytesStorageKey(key))
	if !ok {
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	e, ok := v.(memoryEntry)
	if !ok || e.kind != entryKindBytes {
		// Foreign value type stored at this key — treat as a miss and clear
		// it so subsequent calls don't keep paying the type-assert cost.
		m.store.Delete(bytesStorageKey(key))
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	b, ok := e.value.([]byte)
	if !ok {
		m.store.Delete(bytesStorageKey(key))
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	if record {
		recordHit(endpoint)
	}
	return cloneBytes(b), true
}

// Set stores value at key. A zero ttl is a no-op (it would otherwise create
// an entry that uses go-cache's package default and surprise callers).
func (m *MemoryCache) Set(_ context.Context, key Key, endpoint string, value []byte, ttl time.Duration) {
	if m == nil || ttl <= 0 || len(value) > m.maxValueBytes {
		return
	}
	m.store.Set(bytesStorageKey(key), memoryEntry{
		kind:     entryKindBytes,
		endpoint: endpoint,
		value:    cloneBytes(value),
	}, ttl)
}

// Delete removes the entry for key (best-effort).
func (m *MemoryCache) Delete(_ context.Context, key Key) {
	if m == nil {
		return
	}
	m.store.Delete(bytesStorageKey(key))
}

// ItemCount returns the number of items currently in the cache. Intended for
// tests and debugging only.
func (m *MemoryCache) ItemCount() int {
	if m == nil {
		return 0
	}
	return m.store.ItemCount()
}

// Flush removes every entry from the cache. Intended for tests; production
// code should rely on TTL/eviction or targeted Delete.
func (m *MemoryCache) Flush() {
	if m == nil {
		return
	}
	m.store.Flush()
}

// GetOrFetchBytes is a helper used by the schema response cache:
//  1. Returns a cached value if present.
//  2. Otherwise runs fn (deduped via singleflight per cache instance + key),
//     copies the returned bytes, and stores them.
//
// fn errors are NEVER cached.
func GetOrFetchBytes(
	ctx context.Context,
	c *MemoryCache,
	key Key,
	endpoint string,
	ttl time.Duration,
	fn func(context.Context) ([]byte, error),
) ([]byte, error) {
	if c == nil || ttl <= 0 {
		return fn(ctx)
	}
	if b, ok := c.Get(ctx, key, endpoint); ok {
		return b, nil
	}

	raw, err, _ := c.sf.Do(bytesStorageKey(key), func() (any, error) {
		if b, ok := c.getBytes(key, endpoint, false); ok {
			return b, nil
		}
		b, ferr := fn(ctx)
		if ferr != nil {
			return nil, ferr
		}
		if b == nil {
			return []byte(nil), nil
		}
		c.Set(ctx, key, endpoint, b, ttl)
		return cloneBytes(b), nil
	})
	if err != nil {
		return nil, err
	}
	b, ok := raw.([]byte)
	if !ok {
		return nil, errors.New("schemads/cache: GetOrFetchBytes returned unexpected type")
	}
	return cloneBytes(b), nil
}

// GetOrFetch is a top-level helper for small JSON-serializable in-handler
// sub-fetches. It uses the SDK-owned in-memory cache:
//  1. Returns a cached value if present.
//  2. Otherwise runs fn (deduped via singleflight per cache instance + key),
//     marshals the result to JSON, and stores it.
//
// fn errors are NEVER cached. For hot paths where JSON marshal/unmarshal would
// dominate CPU, use [Typed].
func GetOrFetch[T any](
	ctx context.Context,
	c *MemoryCache,
	key Key,
	endpoint string,
	ttl time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	var zero T
	if c == nil || ttl <= 0 {
		return fn(ctx)
	}
	if b, ok := c.Get(ctx, key, endpoint); ok {
		var v T
		if err := json.Unmarshal(b, &v); err == nil {
			return v, nil
		}
		// Corrupt entry — drop it and fall through to refetch.
		c.Delete(ctx, key)
	}

	raw, err, _ := c.sf.Do(bytesStorageKey(key), func() (any, error) {
		// Double-check after acquiring the singleflight slot — another
		// caller may have already populated the cache.
		if b, ok := c.getBytes(key, endpoint, false); ok {
			var v T
			if uerr := json.Unmarshal(b, &v); uerr == nil {
				return v, nil
			}
			c.Delete(ctx, key)
		}
		v, ferr := fn(ctx)
		if ferr != nil {
			return zero, ferr
		}
		b, merr := json.Marshal(v)
		if merr != nil {
			return zero, merr
		}
		c.Set(ctx, key, endpoint, b, ttl)
		return v, nil
	})
	if err != nil {
		return zero, err
	}
	v, ok := raw.(T)
	if !ok {
		return zero, errors.New("schemads/cache: GetOrFetch returned unexpected type")
	}
	return v, nil
}

func (m *MemoryCache) getTyped(key Key, endpoint string, typ reflect.Type, record bool) (any, bool) {
	if m == nil {
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	storeKey := typedStorageKey(key, endpoint)
	v, ok := m.store.Get(storeKey)
	if !ok {
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	e, ok := v.(memoryEntry)
	if !ok || e.kind != entryKindTyped || e.valueType != typ {
		m.store.Delete(storeKey)
		if record {
			recordMiss(endpoint)
		}
		return nil, false
	}
	if record {
		recordHit(endpoint)
	}
	return e.value, true
}

func (m *MemoryCache) setTyped(key Key, endpoint string, value any, typ reflect.Type, ttl time.Duration) {
	if m == nil || ttl <= 0 {
		return
	}
	m.store.Set(typedStorageKey(key, endpoint), memoryEntry{
		kind:      entryKindTyped,
		endpoint:  endpoint,
		value:     value,
		valueType: typ,
	}, ttl)
}

func getOrFetchTyped[T any](
	ctx context.Context,
	m *MemoryCache,
	key Key,
	endpoint string,
	typ reflect.Type,
	ttl time.Duration,
	fn func(context.Context) (T, error),
) (T, error) {
	if m == nil || ttl <= 0 {
		return fn(ctx)
	}
	if v, ok := m.getTyped(key, endpoint, typ, true); ok {
		out, ok := v.(T)
		if ok {
			return out, nil
		}
		m.deleteTyped(key, endpoint)
		recordMiss(endpoint)
	}

	v, err := m.singleflightDo(typedStorageKey(key, endpoint), func() (any, error) {
		// Double-check after acquiring the singleflight slot.
		if v, ok := m.getTyped(key, endpoint, typ, false); ok {
			out, ok := v.(T)
			if ok {
				return out, nil
			}
			m.deleteTyped(key, endpoint)
		}
		val, ferr := fn(ctx)
		if ferr != nil {
			var zero T
			return zero, ferr
		}
		m.setTyped(key, endpoint, val, typ, ttl)
		return val, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	out, _ := v.(T)
	return out, nil
}

func (m *MemoryCache) deleteTyped(key Key, endpoint string) {
	if m == nil {
		return
	}
	m.store.Delete(typedStorageKey(key, endpoint))
}

func (m *MemoryCache) singleflightDo(key string, fn func() (any, error)) (any, error) {
	if m == nil {
		return fn()
	}
	v, err, _ := m.sf.Do(key, fn)
	return v, err
}

func bytesStorageKey(key Key) string {
	return string(entryKindBytes) + "|" + key.raw()
}

func typedStorageKey(key Key, endpoint string) string {
	return string(entryKindTyped) + "|" + endpoint + "|" + key.raw()
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
