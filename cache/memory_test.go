package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newKeyFor(t *testing.T, parts ...string) Key {
	t.Helper()
	pc := mockPluginContext()
	k, err := KeyFromPluginContext(pc, ScopeDatasource, "test", parts...)
	require.NoError(t, err)
	return k
}

func TestMemoryCache_GetSet(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "a")

	_, ok := c.Get(context.Background(), k, "test")
	require.False(t, ok)

	c.Set(context.Background(), k, "test", []byte("hello"), time.Minute)
	v, ok := c.Get(context.Background(), k, "test")
	require.True(t, ok)
	require.Equal(t, []byte("hello"), v)
}

func TestMemoryCache_ZeroTTLDoesNotStore(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "a")
	c.Set(context.Background(), k, "test", []byte("hello"), 0)
	_, ok := c.Get(context.Background(), k, "test")
	require.False(t, ok, "zero ttl must not store")
}

func TestMemoryCache_Expiry(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute, CleanupInterval: 10 * time.Millisecond})
	k := newKeyFor(t, "a")
	c.Set(context.Background(), k, "test", []byte("hello"), 20*time.Millisecond)
	v, ok := c.Get(context.Background(), k, "test")
	require.True(t, ok)
	require.Equal(t, []byte("hello"), v)

	time.Sleep(60 * time.Millisecond)
	_, ok = c.Get(context.Background(), k, "test")
	require.False(t, ok, "entry should have expired")
}

func TestMemoryCache_Delete(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "a")
	c.Set(context.Background(), k, "test", []byte("hello"), time.Minute)
	c.Delete(context.Background(), k)
	_, ok := c.Get(context.Background(), k, "test")
	require.False(t, ok)
}

func TestMemoryCache_GetSetCopiesBytes(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "copy")
	input := []byte("hello")
	c.Set(context.Background(), k, "test", input, time.Minute)
	input[0] = 'j'

	got, ok := c.Get(context.Background(), k, "test")
	require.True(t, ok)
	require.Equal(t, []byte("hello"), got)

	got[0] = 'y'
	gotAgain, ok := c.Get(context.Background(), k, "test")
	require.True(t, ok)
	require.Equal(t, []byte("hello"), gotAgain)
}

func TestMemoryCache_MaxValueBytesSkipsLargeResponses(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute, MaxValueBytes: 3})
	k := newKeyFor(t, "large")

	c.Set(context.Background(), k, "test", []byte("hello"), time.Minute)
	_, ok := c.Get(context.Background(), k, "test")
	require.False(t, ok)
}

func TestGetOrFetch_DedupesConcurrentMisses(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "sf")

	var calls int32
	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	results := make([]string, N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			v, err := GetOrFetch(context.Background(), c, k, "test", time.Minute, func(ctx context.Context) (string, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(20 * time.Millisecond)
				return "value", nil
			})
			require.NoError(t, err)
			results[i] = v
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "fn should run exactly once under singleflight")
	for _, v := range results {
		require.Equal(t, "value", v)
	}
}

func TestGetOrFetch_DoesNotCacheErrors(t *testing.T) {
	c := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	k := newKeyFor(t, "err")

	var calls int32
	fetch := func() error {
		_, err := GetOrFetch(context.Background(), c, k, "test", time.Minute, func(ctx context.Context) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "", context.Canceled
		})
		return err
	}
	require.Error(t, fetch())
	require.Error(t, fetch())
	require.Equal(t, int32(2), atomic.LoadInt32(&calls), "errors must not be cached")
}

func TestNilMemoryCache_NeverStores(t *testing.T) {
	var c *MemoryCache
	k := newKeyFor(t, "x")
	c.Set(context.Background(), k, "test", []byte("v"), time.Minute)
	_, ok := c.Get(context.Background(), k, "test")
	require.False(t, ok)
}
