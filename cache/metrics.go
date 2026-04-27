package cache

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus metrics, mirroring the SDK instancemgmt naming style.
//
// Labels:
//   - endpoint: the schemads endpoint name (tables, columns, ...) or
//     "subfetch" for in-handler typed caches; callers supply this label.
var (
	hits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "schemads_cache_hits_total",
		Help: "Total number of schemads cache hits.",
	}, []string{"endpoint"})

	misses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "schemads_cache_misses_total",
		Help: "Total number of schemads cache misses.",
	}, []string{"endpoint"})

	evictions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "schemads_cache_evictions_total",
		Help: "Total number of schemads cache entries evicted (TTL or capacity).",
	}, []string{"endpoint"})
)

// MustRegisterMetrics registers the schemads cache metrics with the given
// Prometheus registerer. Safe to call multiple times with the same registerer
// — subsequent calls are no-ops.
//
// If never called, metrics still record but are not exposed; this matches
// other SDK packages and keeps test code free of registration boilerplate.
var registerOnce sync.Once

func MustRegisterMetrics(r prometheus.Registerer) {
	if r == nil {
		r = prometheus.DefaultRegisterer
	}
	registerOnce.Do(func() {
		r.MustRegister(hits, misses, evictions)
	})
}

// recordHit/recordMiss/recordEviction wrap the counters so the rest of the
// package doesn't import prometheus directly.
func recordHit(endpoint string)      { hits.WithLabelValues(endpoint).Inc() }
func recordMiss(endpoint string)     { misses.WithLabelValues(endpoint).Inc() }
func recordEviction(endpoint string) { evictions.WithLabelValues(endpoint).Inc() }
