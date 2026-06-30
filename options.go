package schemas

import (
	"time"

	"github.com/grafana/schemads/cache"
)

// Options tunes [SchemaDatasource] behaviour. Pass to
// [NewSchemaDatasourceWithOptions] to override defaults.
//
// The zero value of Options is NOT "caching off"; it means "use defaults".
// To disable caching entirely:
//
//	NewSchemaDatasourceWithOptions(..., Options{DisableCache: true})
type Options struct {
	// DisableCache disables response-level caching. SchemaDatasource.Cache
	// returns nil when this is true; plugin-owned sub-fetch caches should pass
	// that nil through to cache.NewTyped/cache.GetOrFetch so they also bypass.
	DisableCache bool

	// Cache configures the SDK-owned in-memory cache.
	//
	// The zero value uses [cache.DefaultMemoryOptions]. The same cache instance
	// is returned by [SchemaDatasource.Cache] for plugin in-handler sub-fetches.
	Cache cache.MemoryOptions

	// TTL configures per-endpoint TTLs. A zero TTL for any individual
	// endpoint disables caching for that endpoint only — useful for
	// endpoints whose responses are time-range dependent (ColumnValues).
	//
	// When Options is passed without a TTL block (TTL == EndpointTTLs{}), the
	// defaults from [DefaultOptions].TTL are applied. To disable a single
	// endpoint while keeping the rest of the defaults, copy DefaultOptions
	// and set the field to zero explicitly.
	TTL EndpointTTLs

	// DefaultScope is applied to every endpoint unless overridden in
	// PerEndpointScope. Defaults to [cache.ScopeUser] for safety.
	DefaultScope cache.Scope

	// PerEndpointScope overrides DefaultScope for specific endpoints, keyed
	// by the RequestType* constants from resource.go (e.g. RequestTypeTables).
	//
	// Plugins use this to relax to ScopeDatasource on endpoints that have
	// been audited and confirmed user-independent — typically Tables/Columns
	// for clusters that do not enforce per-user table visibility.
	PerEndpointScope map[string]cache.Scope

	// Refresh configures the manual cache-bypass header.
	Refresh RefreshPolicy
}

// EndpointTTLs holds a TTL for each schemads endpoint. Zero disables caching
// for that endpoint only.
type EndpointTTLs struct {
	FullSchema           time.Duration
	Tables               time.Duration
	Columns              time.Duration
	TableParameterValues time.Duration
	// ColumnValues is intentionally zero in DefaultOptions because the
	// response depends on TimeRange, freshness matters for autocomplete UIs,
	// and values are more likely to contain PII. Plugins must opt in
	// explicitly.
	ColumnValues time.Duration
	Functions    time.Duration
}

// RefreshPolicy controls the manual cache-invalidation header.
//
// When a request carries Header (canonical "X-Schemads-Refresh"), the cached
// entry for that exact key is deleted and the handler re-runs. Bypass is
// rate-limited per (tenant, endpoint) to prevent CPU DOS via constant
// invalidation; requests within MinInterval of the previous bypass for the
// same (tenant, endpoint) fall back to the cached response.
type RefreshPolicy struct {
	// Header is the HTTP header name that triggers a refresh. Empty disables
	// refresh entirely.
	Header string
	// MinInterval is the minimum time between honoured refreshes per
	// (tenant, endpoint). Zero disables rate-limiting.
	MinInterval time.Duration
}

// DefaultOptions captures the safe-by-default behaviour applied when callers
// use NewSchemaDatasource (i.e. no Options passed).
//
// The defaults intentionally cache only the read-mostly endpoints. ColumnValues
// is excluded — see options.go EndpointTTLs.ColumnValues docstring.
var DefaultOptions = Options{
	TTL: EndpointTTLs{
		FullSchema:           5 * time.Minute,
		Tables:               5 * time.Minute,
		Columns:              2 * time.Minute,
		TableParameterValues: 1 * time.Minute,
		ColumnValues:         0, // explicit: never cached by default
		Functions:            5 * time.Minute,
	},
	DefaultScope:     cache.ScopeUser,
	PerEndpointScope: nil,
	Refresh: RefreshPolicy{
		Header:      "X-Schemads-Refresh",
		MinInterval: 5 * time.Second,
	},
}

// resolve fills in any zero fields with the corresponding DefaultOptions
// values. It does NOT recursively merge EndpointTTLs — if a caller supplies a
// TTL block, they take ownership of every field. This keeps re-enabling
// ColumnValues from accidentally re-zeroing the other endpoints.
func (o Options) resolve() Options {
	if o.Cache == (cache.MemoryOptions{}) {
		o.Cache = cache.DefaultMemoryOptions()
	}
	if o.TTL == (EndpointTTLs{}) {
		o.TTL = DefaultOptions.TTL
	}
	if o.Refresh.Header == "" && o.Refresh.MinInterval == 0 {
		o.Refresh = DefaultOptions.Refresh
	}

	return o
}

// scopeFor returns the configured scope for endpoint, falling back to
// DefaultScope.
func (o Options) scopeFor(endpoint string) cache.Scope {
	if s, ok := o.PerEndpointScope[endpoint]; ok {
		return s
	}
	if o.DefaultScope != cache.ScopeUser && o.DefaultScope != cache.ScopeDatasource {
		return cache.ScopeUser
	}
	return o.DefaultScope
}

// ttlFor returns the configured TTL for endpoint. Returns zero (no caching)
// if the endpoint is unknown.
func (o Options) ttlFor(endpoint string) time.Duration {
	switch endpoint {
	case RequestTypeFullSchema:
		return o.TTL.FullSchema
	case RequestTypeTables:
		return o.TTL.Tables
	case RequestTypeColumns:
		return o.TTL.Columns
	case RequestTypeTableParameterValues:
		return o.TTL.TableParameterValues
	case RequestTypeColumnValues:
		return o.TTL.ColumnValues
	case RequestTypeFunctions:
		return o.TTL.Functions
	default:
		return 0
	}
}
