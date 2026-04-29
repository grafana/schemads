package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type wsList []string

func TestTyped_GetOrFetch(t *testing.T) {
	store := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	c := NewTyped[wsList](store, "subfetch:test")
	k := newKeyFor(t, "ws", "sub-1")

	var calls int32
	v, err := c.GetOrFetch(context.Background(), k, time.Minute, func(ctx context.Context) (wsList, error) {
		atomic.AddInt32(&calls, 1)
		return wsList{"a", "b"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, wsList{"a", "b"}, v)

	v2, err := c.GetOrFetch(context.Background(), k, time.Minute, func(ctx context.Context) (wsList, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("should not be called")
	})
	require.NoError(t, err)
	require.Equal(t, wsList{"a", "b"}, v2)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestTyped_DedupesConcurrentMisses(t *testing.T) {
	store := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	c := NewTyped[wsList](store, "subfetch:test")
	k := newKeyFor(t, "ws", "sub-1")

	var calls int32
	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := c.GetOrFetch(context.Background(), k, time.Minute, func(ctx context.Context) (wsList, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(10 * time.Millisecond)
				return wsList{"x"}, nil
			})
			require.NoError(t, err)
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestTyped_Expiry(t *testing.T) {
	store := NewMemory(MemoryOptions{DefaultTTL: time.Minute, CleanupInterval: 10 * time.Millisecond})
	c := NewTyped[string](store, "subfetch:test")
	k := newKeyFor(t, "ttl")
	c.Set(context.Background(), k, "v", 20*time.Millisecond)
	v, ok := c.Get(context.Background(), k)
	require.True(t, ok)
	require.Equal(t, "v", v)
	time.Sleep(40 * time.Millisecond)
	_, ok = c.Get(context.Background(), k)
	require.False(t, ok)
}

func TestTyped_DoesNotCacheErrors(t *testing.T) {
	store := NewMemory(MemoryOptions{DefaultTTL: time.Minute})
	c := NewTyped[string](store, "subfetch:test")
	k := newKeyFor(t, "err")
	var calls int32
	fn := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("boom")
	}
	_, err := c.GetOrFetch(context.Background(), k, time.Minute, fn)
	require.Error(t, err)
	_, err = c.GetOrFetch(context.Background(), k, time.Minute, fn)
	require.Error(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestTyped_NilMemoryCacheBypasses(t *testing.T) {
	c := NewTyped[string](nil, "subfetch:test")
	k := newKeyFor(t, "disabled")
	var calls int32
	fn := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "value", nil
	}

	_, err := c.GetOrFetch(context.Background(), k, time.Minute, fn)
	require.NoError(t, err)
	_, err = c.GetOrFetch(context.Background(), k, time.Minute, fn)
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}
