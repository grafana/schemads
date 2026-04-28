package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/grafana/schemads/cache"
)

// SchemaDatasource is a [backend.CallResourceHandler] that intercepts requests
// under [BaseResourcePath] and delegates them to the configured handlers. All
// other resource paths are forwarded to CallResourceHandler (if set) or return 404.
//
// Response-level caching is on by default (see [DefaultOptions]); plugins can
// tune or disable it with [NewSchemaDatasourceWithOptions].
type SchemaDatasource struct {
	// The Schema handlers serve the different schema endpoints. If nil, those requests
	// return 501 Not Implemented.
	SchemaHandler               SchemaHandler
	TablesHandler               TablesHandler
	ColumnsHandler              ColumnsHandler
	TableParameterValuesHandler TableParameterValuesHandler
	ColumnValuesHandler         ColumnValuesHandler

	// The CallResourceHandler is used to forward requests that are not handled by the schema handlers.
	CallResourceHandler backend.CallResourceHandler

	opts  Options
	cache *cache.MemoryCache

	// refreshLimiterMu guards refreshLimiter. The limiter is keyed by
	// (namespace, endpoint) so a malicious caller within one tenant cannot
	// starve refresh quota for another.
	refreshLimiterMu sync.Mutex
	refreshLimiter   map[string]time.Time
}

// NewSchemaDatasource creates a [SchemaDatasource] with default-on caching.
// Pass nil for any handler to return 501 for that endpoint. Pass nil for next
// to return 404 for non-schema paths.
//
// Equivalent to NewSchemaDatasourceWithOptions(..., DefaultOptions).
func NewSchemaDatasource(schemaHandler SchemaHandler, tablesHandler TablesHandler, columnsHandler ColumnsHandler, tableParameterValuesHandler TableParameterValuesHandler, columnValuesHandler ColumnValuesHandler, next backend.CallResourceHandler) *SchemaDatasource {
	return NewSchemaDatasourceWithOptions(schemaHandler, tablesHandler, columnsHandler, tableParameterValuesHandler, columnValuesHandler, next, DefaultOptions)
}

// NewSchemaDatasourceWithOptions is the tuning form of [NewSchemaDatasource].
// Use it to override TTLs, tune the in-memory cache, relax scopes per endpoint,
// or disable caching entirely:
//
//	// Disable caching:
//	NewSchemaDatasourceWithOptions(..., schemas.Options{DisableCache: true})
//
//	// Relax Tables/Columns to ScopeDatasource for a cluster that doesn't
//	// enforce per-user index visibility:
//	opts := schemas.DefaultOptions
//	opts.PerEndpointScope = map[string]cache.Scope{
//	    schemas.RequestTypeTables:  cache.ScopeDatasource,
//	    schemas.RequestTypeColumns: cache.ScopeDatasource,
//	}
//	NewSchemaDatasourceWithOptions(..., opts)
func NewSchemaDatasourceWithOptions(schemaHandler SchemaHandler, tablesHandler TablesHandler, columnsHandler ColumnsHandler, tableParameterValuesHandler TableParameterValuesHandler, columnValuesHandler ColumnValuesHandler, next backend.CallResourceHandler, opts Options) *SchemaDatasource {
	resolved := opts.resolve()
	var memoryCache *cache.MemoryCache
	if !resolved.DisableCache {
		memoryCache = cache.NewMemory(resolved.Cache)
	}
	return &SchemaDatasource{
		SchemaHandler:               schemaHandler,
		TablesHandler:               tablesHandler,
		ColumnsHandler:              columnsHandler,
		TableParameterValuesHandler: tableParameterValuesHandler,
		ColumnValuesHandler:         columnValuesHandler,
		CallResourceHandler:         next,
		opts:                        resolved,
		cache:                       memoryCache,
		refreshLimiter:              make(map[string]time.Time),
	}
}

// Cache returns the cache instance used by this SchemaDatasource. Plugins use
// it for in-handler sub-fetches (typically wrapped in [cache.Typed]) so
// response and sub-fetch caches share one in-memory store and eviction policy.
//
// Returns nil when caching is disabled.
func (ds *SchemaDatasource) Cache() *cache.MemoryCache {
	return ds.cache
}

// CallResource implements [backend.CallResourceHandler]. Requests whose path
// starts with [BaseResourcePath] are handled internally; everything else is
// forwarded to CallResourceHandler.
func (ds *SchemaDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req != nil && strings.HasPrefix(req.Path, BaseResourcePath) {
		return ds.handleSchemaResource(ctx, req, sender)
	}
	if ds.CallResourceHandler != nil {
		return ds.CallResourceHandler.CallResource(ctx, req, sender)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: 404,
		Body:   []byte("not found"),
	})
}

type parsedSchemaRequest struct {
	schema               *SchemaRequest
	tables               *TablesRequest
	columns              *ColumnsRequest
	tableParameterValues *TableParameterValuesRequest
	columnValues         *ColumnValuesRequest
	keyParts             []string
}

func (ds *SchemaDatasource) handleSchemaResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	path := strings.TrimLeft(strings.TrimPrefix(req.Path, BaseResourcePath), "/")
	headers := extractHeaders(req)

	commonRequest := &CommonRequest{
		Headers:       headers,
		PluginContext: req.PluginContext,
	}

	parsed, err := parseSchemaRequest(path, req.Body, commonRequest)
	if err != nil {
		return err
	}

	ttl := ds.opts.ttlFor(path)
	scope := ds.opts.scopeFor(path)
	logger := log.DefaultLogger.FromContext(ctx)

	// Build the cache key. If we can't (e.g. ScopeUser with nil User), bypass
	// the cache rather than serving anonymous results to authenticated users.
	var (
		key       cache.Key
		keyOK     bool
		cacheable = ttl > 0 && ds.cache != nil
	)
	if cacheable {
		k, err := cache.KeyFromPluginContext(req.PluginContext, scope, path, parsed.keyParts...)
		if err != nil {
			logger.Warn("schemads: cache bypassed", "endpoint", path, "scope", scope, "err", err)
		} else {
			key = k
			keyOK = true
		}
	}

	// Refresh-header path: invalidate the cached entry and re-run the handler.
	// Always rate-limited per (tenant, endpoint) so a malicious caller can't
	// CPU-DOS the upstream. Honour only when caching is enabled and the key
	// could be built (otherwise there's nothing to invalidate).
	if cacheable && keyOK && ds.opts.Refresh.Header != "" {
		if v := req.GetHTTPHeader(ds.opts.Refresh.Header); v != "" {
			if ds.allowRefresh(req.PluginContext.Namespace, path) {
				ds.cache.Delete(ctx, key)
			}
		}
	}

	fetch := func(ctx context.Context) ([]byte, error) {
		return ds.dispatch(ctx, path, sender, parsed)
	}
	var data []byte
	if cacheable && keyOK {
		data, err = cache.GetOrFetchBytes(ctx, ds.cache, key, path, ttl, fetch)
	} else {
		data, err = fetch(ctx)
	}
	if err != nil || data == nil {
		// dispatch already wrote a response (404/501/validation error) or
		// returned a real error. Either way, never cache.
		return err
	}
	return sendJSON(sender, data)
}

// dispatch runs the handler for endpoint and returns the marshaled response
// bytes. If it returns nil bytes with no error, it has already sent a
// 404/501/validation response to sender — the caller MUST NOT cache or
// re-send.
func (ds *SchemaDatasource) dispatch(ctx context.Context, endpoint string, sender backend.CallResourceResponseSender, parsed *parsedSchemaRequest) ([]byte, error) {
	switch endpoint {
	case RequestTypeFullSchema:
		if ds.SchemaHandler == nil {
			return nil, sendSchemaError(sender, http.StatusNotImplemented, ErrSchemaNotImplemented.Error())
		}
		response, err := ds.SchemaHandler.Schema(ctx, parsed.schema)
		if err != nil {
			return nil, err
		}
		if err := ValidateSchema(response.FullSchema); err != nil {
			return nil, sendSchemaError(sender, http.StatusInternalServerError, "schema validation failed: "+err.Error())
		}
		return json.Marshal(response)

	case RequestTypeTables:
		if ds.TablesHandler == nil {
			return nil, sendSchemaError(sender, http.StatusNotImplemented, ErrTablesNotImplemented.Error())
		}
		response, err := ds.TablesHandler.Tables(ctx, parsed.tables)
		if err != nil {
			return nil, err
		}
		return json.Marshal(response)

	case RequestTypeColumns:
		if ds.ColumnsHandler == nil {
			return nil, sendSchemaError(sender, http.StatusNotImplemented, ErrColumnsNotImplemented.Error())
		}
		response, err := ds.ColumnsHandler.Columns(ctx, parsed.columns)
		if err != nil {
			return nil, err
		}
		return json.Marshal(response)

	case RequestTypeTableParameterValues:
		if ds.TableParameterValuesHandler == nil {
			return nil, sendSchemaError(sender, http.StatusNotImplemented, ErrTableParameterValuesNotImplemented.Error())
		}
		response, err := ds.TableParameterValuesHandler.TableParameterValues(ctx, parsed.tableParameterValues)
		if err != nil {
			return nil, err
		}
		return json.Marshal(response)

	case RequestTypeColumnValues:
		if ds.ColumnValuesHandler == nil {
			return nil, sendSchemaError(sender, http.StatusNotImplemented, ErrColumnValuesNotImplemented.Error())
		}
		response, err := ds.ColumnValuesHandler.ColumnValues(ctx, parsed.columnValues)
		if err != nil {
			return nil, err
		}
		return json.Marshal(response)

	default:
		return nil, sender.Send(&backend.CallResourceResponse{
			Status: 404,
			Body:   []byte(ErrSchemaNotImplemented.Error()),
		})
	}
}

func parseSchemaRequest(endpoint string, body []byte, common *CommonRequest) (*parsedSchemaRequest, error) {
	parsed := &parsedSchemaRequest{}
	switch endpoint {
	case RequestTypeFullSchema:
		parsed.schema = &SchemaRequest{CommonRequest: *common}
		parsed.keyParts = []string{"request"}
	case RequestTypeTables:
		request := &TablesRequest{CommonRequest: *common}
		if len(body) > 0 {
			if err := json.Unmarshal(body, request); err != nil {
				return nil, err
			}
		}
		parsed.tables = request
		parsed.keyParts = []string{"request"}
	case RequestTypeColumns:
		request := &ColumnsRequest{CommonRequest: *common}
		if err := json.Unmarshal(body, request); err != nil {
			return nil, err
		}
		parsed.columns = request
		parsed.keyParts = []string{
			"tables", canonicalJSON(sortedStrings(request.Tables)),
			"tableParameters", canonicalJSON(request.TableParameters),
		}
	case RequestTypeTableParameterValues:
		request := &TableParameterValuesRequest{CommonRequest: *common}
		if err := json.Unmarshal(body, request); err != nil {
			return nil, err
		}
		parsed.tableParameterValues = request
		parsed.keyParts = []string{
			"table", request.Table,
			"tableParameter", request.TableParameter,
			"dependencyValues", canonicalJSON(request.DependencyValues),
		}
	case RequestTypeColumnValues:
		request := &ColumnValuesRequest{CommonRequest: *common}
		if err := json.Unmarshal(body, request); err != nil {
			return nil, err
		}
		parsed.columnValues = request
		parsed.keyParts = []string{
			"table", request.Table,
			"columns", canonicalJSON(sortedStrings(request.Columns)),
			"tableParameters", canonicalJSON(request.TableParameters),
			"timeRange", canonicalJSON(request.TimeRange),
		}
	default:
		parsed.keyParts = []string{"unknown"}
	}
	return parsed, nil
}

func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// allowRefresh returns true if a refresh for (namespace, endpoint) is allowed
// under the configured rate limit. Updates the limiter on success so the
// next call within MinInterval is rejected.
func (ds *SchemaDatasource) allowRefresh(namespace, endpoint string) bool {
	if ds.opts.Refresh.MinInterval <= 0 {
		return true
	}
	bucket := fmt.Sprintf("%s|%s", namespace, endpoint)
	ds.refreshLimiterMu.Lock()
	defer ds.refreshLimiterMu.Unlock()
	now := time.Now()
	if last, ok := ds.refreshLimiter[bucket]; ok {
		if now.Sub(last) < ds.opts.Refresh.MinInterval {
			return false
		}
	}
	ds.refreshLimiter[bucket] = now
	return true
}

func sendJSON(sender backend.CallResourceResponseSender, data []byte) error {
	return sender.Send(&backend.CallResourceResponse{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: data,
	})
}
