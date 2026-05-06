package schemas

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/schemads/cache"
)

// --- handler stubs ----------------------------------------------------------

type stubSchemaHandler struct {
	calls int32
	fn    func(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)
}

func (h *stubSchemaHandler) Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
	atomic.AddInt32(&h.calls, 1)
	if h.fn != nil {
		return h.fn(ctx, req)
	}
	return &SchemaResponse{FullSchema: &Schema{Tables: []Table{{Name: "t1"}}}}, nil
}

type stubTablesHandler struct {
	calls int32
	fn    func(ctx context.Context, req *TablesRequest) (*TablesResponse, error)
}

func (h *stubTablesHandler) Tables(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
	atomic.AddInt32(&h.calls, 1)
	if h.fn != nil {
		return h.fn(ctx, req)
	}
	return &TablesResponse{Tables: []string{"t1", "t2"}}, nil
}

type stubColumnsHandler struct {
	calls int32
	fn    func(ctx context.Context, req *ColumnsRequest) (*ColumnsResponse, error)
}

func (h *stubColumnsHandler) Columns(ctx context.Context, req *ColumnsRequest) (*ColumnsResponse, error) {
	atomic.AddInt32(&h.calls, 1)
	if h.fn != nil {
		return h.fn(ctx, req)
	}
	return &ColumnsResponse{Columns: map[string][]Column{"t1": {{Name: "c1", Type: ColumnTypeString}}}}, nil
}

type stubColumnValuesHandler struct {
	calls int32
}

func (h *stubColumnValuesHandler) ColumnValues(ctx context.Context, req *ColumnValuesRequest) (*ColumnValuesResponse, error) {
	atomic.AddInt32(&h.calls, 1)
	return &ColumnValuesResponse{ColumnValues: map[string][]string{"c1": {"a", "b"}}}, nil
}

// --- response sender --------------------------------------------------------

type captureSender struct {
	mu        sync.Mutex
	responses []*backend.CallResourceResponse
}

func (s *captureSender) Send(r *backend.CallResourceResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, r)
	return nil
}

func (s *captureSender) last() *backend.CallResourceResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.responses[len(s.responses)-1]
}

// --- helpers ----------------------------------------------------------------

func basePluginContext() backend.PluginContext {
	return backend.PluginContext{
		Namespace: "stack-1",
		DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
			UID:     "ds-42",
			Updated: time.Unix(1_700_000_000, 0),
		},
		User: &backend.User{Login: "alice"},
	}
}

func callResource(t *testing.T, ds *SchemaDatasource, pc backend.PluginContext, endpoint string, body []byte, headers map[string][]string) *backend.CallResourceResponse {
	t.Helper()
	sender := &captureSender{}
	req := &backend.CallResourceRequest{
		PluginContext: pc,
		Path:          BaseResourcePath + "/" + endpoint,
		Method:        "POST",
		Body:          body,
		Headers:       headers,
	}
	require.NoError(t, ds.CallResource(context.Background(), req, sender))
	return sender.last()
}

// --- tests ------------------------------------------------------------------

func TestDefaultOn_TablesCachesResponse(t *testing.T) {
	tables := &stubTablesHandler{}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pc := basePluginContext()
	r1 := callResource(t, ds, pc, RequestTypeTables, nil, nil)
	r2 := callResource(t, ds, pc, RequestTypeTables, nil, nil)

	require.Equal(t, 200, r1.Status)
	require.Equal(t, 200, r2.Status)
	require.Equal(t, r1.Body, r2.Body)
	require.Equal(t, int32(1), atomic.LoadInt32(&tables.calls), "second identical request must hit cache")
}

func TestColumnsCacheKeySupportsTableSlicesAndParams(t *testing.T) {
	columns := &stubColumnsHandler{}
	ds := NewSchemaDatasource(nil, nil, columns, nil, nil, nil)
	pc := basePluginContext()

	bodyA, _ := json.Marshal(ColumnsRequest{
		Tables:          []string{"t2", "t1"},
		TableParameters: map[string]string{"env": "prod"},
	})
	bodyB, _ := json.Marshal(ColumnsRequest{
		Tables:          []string{"t1", "t2"},
		TableParameters: map[string]string{"env": "prod"},
	})
	bodyC, _ := json.Marshal(ColumnsRequest{
		Tables:          []string{"t1", "t2"},
		TableParameters: map[string]string{"env": "dev"},
	})

	callResource(t, ds, pc, RequestTypeColumns, bodyA, nil)
	callResource(t, ds, pc, RequestTypeColumns, bodyB, nil)
	require.Equal(t, int32(1), atomic.LoadInt32(&columns.calls), "same table slice and params must hit cache")

	callResource(t, ds, pc, RequestTypeColumns, bodyC, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&columns.calls), "different user-supplied params must miss cache")
}

func TestResponseCache_EmitsEndpointMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	cache.MustRegisterMetrics(reg)

	tables := &stubTablesHandler{}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)
	pc := basePluginContext()
	beforeFamilies, err := reg.Gather()
	require.NoError(t, err)
	beforeMisses := metricValue(beforeFamilies, "schemads_cache_misses_total", RequestTypeTables)
	beforeHits := metricValue(beforeFamilies, "schemads_cache_hits_total", RequestTypeTables)

	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, nil)

	families, err := reg.Gather()
	require.NoError(t, err)
	require.Equal(t, float64(1), metricValue(families, "schemads_cache_misses_total", RequestTypeTables)-beforeMisses)
	require.Equal(t, float64(1), metricValue(families, "schemads_cache_hits_total", RequestTypeTables)-beforeHits)
	require.Equal(t, float64(0), metricValue(families, "schemads_cache_hits_total", "response"))
}

func TestResponseCache_DedupesConcurrentMisses(t *testing.T) {
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			time.Sleep(20 * time.Millisecond)
			return &TablesResponse{Tables: []string{"t1"}}, nil
		},
	}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)
	pc := basePluginContext()

	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			sender := &captureSender{}
			req := &backend.CallResourceRequest{
				PluginContext: pc,
				Path:          BaseResourcePath + "/" + RequestTypeTables,
				Method:        "POST",
			}
			errs[i] = ds.CallResource(context.Background(), req, sender)
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&tables.calls), "concurrent identical misses must be deduped")
}

func TestColumnValues_NotCachedByDefault(t *testing.T) {
	cv := &stubColumnValuesHandler{}
	ds := NewSchemaDatasource(nil, nil, nil, nil, cv, nil)

	pc := basePluginContext()
	body, _ := json.Marshal(ColumnValuesRequest{Table: "t1", Columns: []string{"c1"}})

	callResource(t, ds, pc, RequestTypeColumnValues, body, nil)
	callResource(t, ds, pc, RequestTypeColumnValues, body, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&cv.calls), "ColumnValues must NOT be cached by default")
}

func TestColumnValues_CachedWhenOptedIn(t *testing.T) {
	cv := &stubColumnValuesHandler{}
	opts := DefaultOptions
	opts.TTL.ColumnValues = time.Minute
	ds := NewSchemaDatasourceWithOptions(nil, nil, nil, nil, cv, nil, opts)

	pc := basePluginContext()
	body, _ := json.Marshal(ColumnValuesRequest{Table: "t1", Columns: []string{"c1"}})

	callResource(t, ds, pc, RequestTypeColumnValues, body, nil)
	callResource(t, ds, pc, RequestTypeColumnValues, body, nil)
	require.Equal(t, int32(1), atomic.LoadInt32(&cv.calls), "ColumnValues must be cached when explicitly opted in")
}

func TestTenantIsolation(t *testing.T) {
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			// Return different content per tenant so a leaked entry would be visible.
			return &TablesResponse{Tables: []string{req.PluginContext.Namespace}}, nil
		},
	}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pcA := basePluginContext()
	pcB := basePluginContext()
	pcB.Namespace = "stack-2"

	rA := callResource(t, ds, pcA, RequestTypeTables, nil, nil)
	rB := callResource(t, ds, pcB, RequestTypeTables, nil, nil)
	require.NotEqual(t, rA.Body, rB.Body, "different namespaces must NOT share cached responses")
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls))
}

func TestUserScopeSeparatesUsers(t *testing.T) {
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			return &TablesResponse{Tables: []string{req.PluginContext.User.Login}}, nil
		},
	}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pcA := basePluginContext()
	pcB := basePluginContext()
	pcB.User = &backend.User{Login: "bob"}

	rA := callResource(t, ds, pcA, RequestTypeTables, nil, nil)
	rB := callResource(t, ds, pcB, RequestTypeTables, nil, nil)
	require.NotEqual(t, rA.Body, rB.Body)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls))
}

func TestPartialOptionsDefaultToUserScope(t *testing.T) {
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			return &TablesResponse{Tables: []string{req.PluginContext.User.Login}}, nil
		},
	}
	ds := NewSchemaDatasourceWithOptions(nil, tables, nil, nil, nil, nil, Options{
		Cache: cache.MemoryOptions{MaxValueBytes: 5 << 20},
	})

	pcA := basePluginContext()
	pcB := basePluginContext()
	pcB.User = &backend.User{Login: "bob"}

	rA := callResource(t, ds, pcA, RequestTypeTables, nil, nil)
	rB := callResource(t, ds, pcB, RequestTypeTables, nil, nil)
	require.NotEqual(t, rA.Body, rB.Body)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls))
}

func TestPerEndpointScope_RelaxesToDatasource(t *testing.T) {
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			return &TablesResponse{Tables: []string{"t1"}}, nil
		},
	}
	opts := DefaultOptions
	opts.PerEndpointScope = map[string]cache.Scope{
		RequestTypeTables: cache.ScopeDatasource,
	}
	ds := NewSchemaDatasourceWithOptions(nil, tables, nil, nil, nil, nil, opts)

	pcA := basePluginContext()
	pcB := basePluginContext()
	pcB.User = &backend.User{Login: "bob"}

	callResource(t, ds, pcA, RequestTypeTables, nil, nil)
	callResource(t, ds, pcB, RequestTypeTables, nil, nil)
	require.Equal(t, int32(1), atomic.LoadInt32(&tables.calls), "ScopeDatasource must share entries across users")
}

func TestUpdatedTimestampInvalidates(t *testing.T) {
	tables := &stubTablesHandler{}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pc := basePluginContext()
	callResource(t, ds, pc, RequestTypeTables, nil, nil)

	pc2 := basePluginContext()
	dsi := *pc2.DataSourceInstanceSettings
	dsi.Updated = dsi.Updated.Add(time.Second)
	pc2.DataSourceInstanceSettings = &dsi

	callResource(t, ds, pc2, RequestTypeTables, nil, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls), "changed Updated must invalidate")
}

func TestScopeUser_NilUserBypassesCache(t *testing.T) {
	tables := &stubTablesHandler{}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pc := basePluginContext()
	pc.User = nil
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls), "nil User under ScopeUser must bypass cache")
}

func TestScopeUser_EmptyUserIdentityBypassesCache(t *testing.T) {
	tables := &stubTablesHandler{}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pc := basePluginContext()
	pc.User = &backend.User{}
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls), "empty User identity under ScopeUser must bypass cache")
}

func TestDisableCacheDisablesCache(t *testing.T) {
	tables := &stubTablesHandler{}
	ds := NewSchemaDatasourceWithOptions(nil, tables, nil, nil, nil, nil, Options{DisableCache: true})

	pc := basePluginContext()
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls))
}

func TestErrorsAreNotCached(t *testing.T) {
	var calls int32
	tables := &stubTablesHandler{
		fn: func(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
			atomic.AddInt32(&calls, 1)
			return nil, errors.New("boom")
		},
	}
	ds := NewSchemaDatasource(nil, tables, nil, nil, nil, nil)

	pc := basePluginContext()
	sender := &captureSender{}
	req := &backend.CallResourceRequest{PluginContext: pc, Path: BaseResourcePath + "/" + RequestTypeTables, Method: "POST"}
	require.Error(t, ds.CallResource(context.Background(), req, sender))
	require.Error(t, ds.CallResource(context.Background(), req, sender))
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestSchemaValidationFailureNotCached(t *testing.T) {
	// Duplicate table names trigger ValidateSchema failure.
	bad := &Schema{Tables: []Table{{Name: "dup"}, {Name: "dup"}}}
	schema := &stubSchemaHandler{
		fn: func(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
			return &SchemaResponse{FullSchema: bad}, nil
		},
	}
	ds := NewSchemaDatasource(schema, nil, nil, nil, nil, nil)

	pc := basePluginContext()
	r1 := callResource(t, ds, pc, RequestTypeFullSchema, nil, nil)
	r2 := callResource(t, ds, pc, RequestTypeFullSchema, nil, nil)
	require.Equal(t, 500, r1.Status)
	require.Equal(t, 500, r2.Status)
	require.Equal(t, int32(2), atomic.LoadInt32(&schema.calls), "validation failure must not be cached")
}

func TestRefreshHeaderBypassesAndInvalidates(t *testing.T) {
	tables := &stubTablesHandler{}
	opts := DefaultOptions
	opts.Refresh.MinInterval = 0 // disable rate-limit for this test
	ds := NewSchemaDatasourceWithOptions(nil, tables, nil, nil, nil, nil, opts)

	pc := basePluginContext()
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	require.Equal(t, int32(1), atomic.LoadInt32(&tables.calls))

	callResource(t, ds, pc, RequestTypeTables, nil, map[string][]string{"X-Schemads-Refresh": {"1"}})
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls), "refresh header must re-invoke handler")

	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls), "post-refresh value must be re-cached")
}

func TestRefreshHeaderRateLimited(t *testing.T) {
	tables := &stubTablesHandler{}
	opts := DefaultOptions
	opts.Refresh.MinInterval = time.Hour // any refresh after the first is rejected
	ds := NewSchemaDatasourceWithOptions(nil, tables, nil, nil, nil, nil, opts)

	pc := basePluginContext()
	callResource(t, ds, pc, RequestTypeTables, nil, nil)
	callResource(t, ds, pc, RequestTypeTables, nil, map[string][]string{"X-Schemads-Refresh": {"1"}})
	callResource(t, ds, pc, RequestTypeTables, nil, map[string][]string{"X-Schemads-Refresh": {"1"}})

	// First call populates, first refresh honoured (handler runs again),
	// second refresh rate-limited so cached value is returned.
	require.Equal(t, int32(2), atomic.LoadInt32(&tables.calls))
}

func TestCacheAccessor_AlwaysNonNil(t *testing.T) {
	ds := NewSchemaDatasource(nil, nil, nil, nil, nil, nil)
	require.NotNil(t, ds.Cache())

	dsDisabled := NewSchemaDatasourceWithOptions(nil, nil, nil, nil, nil, nil, Options{DisableCache: true})
	require.Nil(t, dsDisabled.Cache())
}

func metricValue(families []*dto.MetricFamily, name, endpoint string) float64 {
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "endpoint" && label.GetValue() == endpoint {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}
