# schemads

> [!CAUTION]
> This repository is experimental and in progress. Do not use this module.
>
> **Note:** as of the next release, response-level caching is **on by default** when you call `NewSchemaDatasource`. See the [Caching](#caching) section for the default TTLs, scope behaviour, and how to disable or tune.

A wrapper for Grafana data source plugins that adds **schema discovery** via `CallResource`. Plugins implement handler interfaces (`SchemaHandler`, `TablesHandler`, `ColumnsHandler`, etc.) and wire them into `NewSchemaDatasource`; consumers use the `Client` to fetch table, column, table parameter, and value metadata.

## Overview

- **Resource base path:** `abstractionSchema` (`BaseResourcePath`), with sub-paths for each endpoint (e.g. `abstractionSchema/tables`).
- **Handler configured:** the wrapper calls the relevant handler and returns the response as JSON.
- **Handler not configured:** returns **501 Not Implemented** with an endpoint-specific sentinel error.
- **Other paths:** forwarded to the wrapped `CallResourceHandler`, or **404** if none is set.

## Plugin integration

### Handler interfaces

Each endpoint has its own handler interface. Implement the ones your plugin supports:

```go
type SchemaHandler interface {
    Schema(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error)
}

type TablesHandler interface {
    Tables(ctx context.Context, req *schemas.TablesRequest) (*schemas.TablesResponse, error)
}

type ColumnsHandler interface {
    Columns(ctx context.Context, req *schemas.ColumnsRequest) (*schemas.ColumnsResponse, error)
}

type TableParameterValuesHandler interface {
    TableParameterValues(ctx context.Context, req *schemas.TableParameterValuesRequest) (*schemas.TableParametersValuesResponse, error)
}

type ColumnValuesHandler interface {
    ColumnValues(ctx context.Context, req *schemas.ColumnValuesRequest) (*schemas.ColumnValuesResponse, error)
}
```

### Wiring into the plugin

`NewSchemaDatasource` returns a `*SchemaDatasource` that implements `backend.CallResourceHandler`. Pass `nil` for any handler your plugin does not support (those endpoints will return 501):

```go
func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
    ds := &MyDatasource{}

    schemaDs := schemas.NewSchemaDatasource(
        schemaHandler,
        tablesHandler,
        columnsHandler,
        tableParameterValuesHandler,
        columnValuesHandler
        nil,  // next CallResourceHandler (or an existing handler)
    )
    return &MyInstance{SchemaDatasource: schemaDs}, nil
}
```

### Adding schema support to an existing data source

If your plugin already implements `backend.CallResourceHandler` (e.g. for health checks, autocomplete, or custom endpoints), pass it as the last argument. Schema requests are intercepted; everything else is forwarded to your existing handler unchanged:

```go
type MyDatasource struct {
    schemas.SchemaDatasource
}

func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
    ds := &MyDatasource{}

    schemaDs := schemas.NewSchemaDatasource(
        ds,   // SchemaHandler
        ds,   // TablesHandler
        ds,   // ColumnsHandler
        ds,   // TableParameterValuesHandler
        ds,   // ColumnValuesHandler
        backend.CallResourceHandlerFunc(ds.handleCustomResource),
    )
    ds.SchemaDatasource = *schemaDs

    return ds, nil
}

func (ds *MyDatasource) handleCustomResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
    switch req.Path {
    case "custom-endpoint":
        return sender.Send(&backend.CallResourceResponse{
            Status: 200,
            Body:   []byte(`{"ok": true}`),
        })
    default:
        return sender.Send(&backend.CallResourceResponse{
            Status: 404,
            Body:   []byte("not found"),
        })
    }
}
```

With this setup:

- `POST /abstractionSchema/tables` (and other schema sub-paths) are handled by the schema handlers.
- All other paths (e.g. `/custom-endpoint`) are forwarded to `handleCustomResource`.

To opt out of schema support entirely, pass `nil` for all handlers — schema requests return 501 and everything else is forwarded:

```go
schemaDs := schemas.NewSchemaDatasource(nil, nil, nil, nil, nil, existingHandler)
```

## Client usage

Create a `Client` with `NewClient`, then call the method for each endpoint. Both `httpClient` and `baseURL` are required. Headers on each request type are forwarded as HTTP headers.

```go
client, err := schemas.NewClient(httpClient, "https://host/api/ds/uid/resources/abstractionSchema")
```

Fetching tables:

```go
resp, err := client.FetchTables(ctx, &schemas.TablesRequest{})
// resp.Tables    – []string
// resp.TableParameters – map[string][]TableParameter
```

Fetching columns for specific tables:

```go
resp, err := client.FetchColumns(ctx, &schemas.ColumnsRequest{
    Tables: []string{"issues"},
})
// resp.Columns – map[string][]Column
```

Fetching columns scoped by table parameters:

```go
resp, err := client.FetchColumns(ctx, &schemas.ColumnsRequest{
    Tables:          []string{"issues"},
    TableParameters: map[string]string{"organization": "grafana", "repository": "grafana"},
})
```

Fetching table parameter values:

```go
resp, err := client.FetchTableParameterValues(ctx, &schemas.TableParameterValuesRequest{
    Table:            "issues",
    TableParameter:   "repository",
    DependencyValues: map[string]string{"organization": "grafana"},
})
// resp.TableParameterValues – map[string][]string
```

Fetching column values:

```go
resp, err := client.FetchColumnValues(ctx, &schemas.ColumnValuesRequest{
    Table:   "issues",
    Columns: []string{"status"},
})
// resp.ColumnValues – map[string][]string
```

Fetching the full schema:

```go
resp, err := client.FetchSchema(ctx, &schemas.SchemaRequest{})
// resp.FullSchema – *Schema
```

## Table parameters

Table parameters model hierarchical scoping parameters that consumers resolve before querying a table (e.g. a GitHub data source requires selecting an **organization** and then a **repository** before querying **issues**).

### Definition

Each `TableParameter` has:

| Field       | Type       | Description                                                   |
| ----------- | ---------- | ------------------------------------------------------------- |
| `Name`      | `string`   | Unique name within the parent table.                          |
| `DependsOn` | `[]string` | Sibling table parameters whose values must be selected first. |
| `Root`      | `bool`     | Entry point with no dependencies. At least one is required.   |
| `Required`  | `bool`     | Must be resolved before the table can be queried.             |

### Validation

`ValidateSchema` is called automatically for `fullSchema` responses and enforces:

1. **Unique names** – no two table parameters in a table share a name.
2. **Valid references** – every `DependsOn` entry names a sibling table parameter.
3. **Acyclic graph** – the dependency graph has no cycles.
4. **Root rules** – at least one table parameter is `Root` and root table parameters have no dependencies.
5. **Required chain** – a required table parameter may only depend on other required table parameters.
6. **TableParameterValues integrity** – `Schema.TableParameterValues` keys reference existing tables and root table parameters only. Non-root table parameter values depend on ancestor selections and must be fetched incrementally via `tableParameterValues` with `DependencyValues`.

Call `ValidateSchema` directly to validate schemas you construct manually.

### Composite keys

Table parameter values in `Schema.TableParametereValues` use composite keys of the form `table_tableParameter` — e.g. `"issues_organization"`.

> **Note:** table and table parameter names may contain underscores, making this separator ambiguous. A future version will adopt a safer delimiter once the protocol is versioned.

## Endpoints

| Path                   | Constant                          | Request type                  | Response type                   |
| ---------------------- | --------------------------------- | ----------------------------- | ------------------------------- |
| `fullSchema`           | `RequestTypeFullSchema`           | `SchemaRequest`               | `SchemaResponse`                |
| `tables`               | `RequestTypeTables`               | `TablesRequest`               | `TablesResponse`                |
| `columns`              | `RequestTypeColumns`              | `ColumnsRequest`              | `ColumnsResponse`               |
| `columnValues`         | `RequestTypeColumnValues`         | `ColumnValuesRequest`         | `ColumnValuesResponse`          |
| `tableParameterValues` | `RequestTypeTableParameterValues` | `TableParameterValuesRequest` | `TableParametersValuesResponse` |

## Caching

`schemads` includes a tenant-safe in-memory response cache that is **on by default** the moment a plugin calls `NewSchemaDatasource`. It is backed by `patrickmn/go-cache` with TTL cleanup and bounded entry/value policies. Plugins can also reuse the same cache instance for in-handler sub-fetches (per-subscription workspaces, per-index field caps, etc.) via `cache.Typed`.

### Default behaviour

Calling the standard constructor enables caching with sensible per-endpoint defaults:

| Endpoint               | Default TTL              | Default scope     |
| ---------------------- | ------------------------ | ----------------- |
| `fullSchema`           | 5 min                    | `cache.ScopeUser` |
| `tables`               | 5 min                    | `cache.ScopeUser` |
| `columns`              | 2 min                    | `cache.ScopeUser` |
| `tableParameterValues` | 1 min                    | `cache.ScopeUser` |
| `columnValues`         | **disabled (TTL = 0)**   | `cache.ScopeUser` |

`columnValues` is intentionally excluded from the default cache because:

1. **Time-range dependent.** `ColumnValuesRequest` carries a `TimeRange`, and values legitimately change with it (e.g. distinct status codes seen in the last 5m vs the last hour).
2. **Freshness-sensitive.** Column values back autocomplete dropdowns; stale values produce user-visible bugs.
3. **Higher PII risk.** Values often contain user-identifying data; shorter exposure is the conservative default.

Cache keys mirror the SDK `instancemgmt` convention so reconfiguration auto-invalidates entries:

```text
namespace#dsUID#updated#proxyHash[#userHash]#sha256(endpoint|minimalTypedFields)
```

The user component is hashed before it reaches the key string, so keys are safe to log. This means **rotating credentials, swapping the PDC tunnel, or editing datasource settings all invalidate cached entries automatically** — there is no need to flush manually.

### Tuning via `Options`

Use `NewSchemaDatasourceWithOptions` to override defaults:

```go
import (
    schemas "github.com/grafana/schemads"
    "github.com/grafana/schemads/cache"
)

opts := schemas.DefaultOptions
// Re-enable ColumnValues with a short TTL — autocomplete results need to feel fresh.
opts.TTL.ColumnValues = 30 * time.Second
// Tune the shared in-memory cache.
opts.Cache.MaxEntries = 2048
opts.Cache.MaxValueBytes = 512 * 1024

ds := schemas.NewSchemaDatasourceWithOptions(
    schemaHandler, tablesHandler, columnsHandler,
    tableParameterValuesHandler, columnValuesHandler,
    next,
    opts,
)
```

> **Note:** `EndpointTTLs` is a flat struct (not an overlay map). When you set any TTL, copy `DefaultOptions` first so you don't accidentally zero out the others.

#### Disabling caching entirely

```go
ds := schemas.NewSchemaDatasourceWithOptions(..., schemas.Options{
    DisableCache: true,
})
```

#### Manually invalidating an entry

Send the request again with the refresh header configured in `RefreshPolicy.Header` (default `X-Schemads-Refresh`). The wrapper deletes the cached entry, runs the handler, and re-caches the result. Bypass is rate-limited per `(tenant, endpoint)` (default 5s) to prevent CPU DoS via constant invalidation.

### Choosing a scope

The default `ScopeUser` is the safe choice. You can relax to `ScopeDatasource` per endpoint via `Options.PerEndpointScope`, **only after auditing** that the endpoint's results do not depend on the calling user's permissions.

Use this checklist:

| Question                                                                                                            | If yes...                          |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------- |
| Does the upstream filter results by the calling user's identity, role, or group membership?                         | Keep `ScopeUser` (default)         |
| Does the upstream apply RBAC or row-level security tied to the impersonated user?                                   | Keep `ScopeUser` (default)         |
| Does the response contain PII that should not be visible to all users with read access to the datasource?           | Keep `ScopeUser` (default)         |
| Is the response a function purely of `(datasource, request body)` — same input, same output, regardless of caller?  | Relax to `ScopeDatasource`         |

Example — Tables/Columns shared across users on a cluster that does not enforce per-user index visibility:

```go
opts := schemas.DefaultOptions
opts.PerEndpointScope = map[string]cache.Scope{
    schemas.RequestTypeTables:  cache.ScopeDatasource,
    schemas.RequestTypeColumns: cache.ScopeDatasource,
}
```

When `ScopeUser` is configured but the request has no user (alerting, public dashboards, recorded queries), the wrapper **bypasses the cache** and logs a warning rather than serving anonymous results to authenticated users.

### In-handler sub-fetch caching

Plugins frequently call upstream APIs from inside their handlers (Azure's per-subscription workspaces, Elasticsearch's per-index `_field_caps`). Use `cache.Typed[T]` to memoize these without paying JSON marshal cost:

```go
type Plugin struct {
    schemaDS    *schemas.SchemaDatasource
    workspaces  *cache.Typed[[]Workspace]
}

func NewPlugin(...) *Plugin {
    schemaDS := schemas.NewSchemaDatasource(...)
    return &Plugin{
        schemaDS:   schemaDS,
        // Tag with a label for Prometheus metrics.
        workspaces: cache.NewTyped[[]Workspace](schemaDS.Cache(), "subfetch:workspaces"),
    }
}

func (p *Plugin) listWorkspaces(ctx context.Context, pc backend.PluginContext, sub string) ([]Workspace, error) {
    // Build a tenant-safe key. ScopeUser is recommended for any subscription-
    // scoped lookup since the user's role determines visible workspaces.
    key, err := cache.KeyFromPluginContext(pc, cache.ScopeUser, "workspaces", sub)
    if err != nil {
        // Fall back to direct fetch (e.g. nil User in alerting context).
        return fetchWorkspaces(ctx, sub)
    }
    return p.workspaces.GetOrFetch(ctx, key, 5*time.Minute, func(ctx context.Context) ([]Workspace, error) {
        return fetchWorkspaces(ctx, sub)
    })
}
```

`Typed[T]` deduplicates concurrent misses for the same key with `singleflight`, never caches errors, and increments `schemads_cache_*_total{endpoint="subfetch:workspaces"}` Prometheus counters. Scope is controlled by the `cache.Key` you pass in, so the same typed cache can safely hold `ScopeUser` and `ScopeDatasource` entries when keys are built correctly.

For small JSON-serializable values, plugins can also use the byte-oriented `cache.GetOrFetch` helper against `ds.Cache()`:

```go
v, err := cache.GetOrFetch(ctx, ds.Cache(), key, "subfetch:workspaces", 5*time.Minute, fetchFn)
```

This is appropriate when the cached value is small/serializable and JSON marshal/unmarshal cost is negligible; `Typed[T]` is preferred for hot paths where marshal/unmarshal would dominate CPU.

### Metrics

The cache exports the following Prometheus counters:

| Metric                           | Labels     | Meaning                                      |
| -------------------------------- | ---------- | -------------------------------------------- |
| `schemads_cache_hits_total`      | `endpoint` | Cache hits per endpoint (or sub-fetch label) |
| `schemads_cache_misses_total`    | `endpoint` | Cache misses                                 |
| `schemads_cache_evictions_total` | `endpoint` | TTL and capacity evictions                   |

Register them with your Prometheus registry once at startup:

```go
cache.MustRegisterMetrics(prometheus.DefaultRegisterer)
```

If never registered, metrics still record but are not exposed (matches other SDK packages).

### Eviction model

- **TTL.** Entries expire after their per-endpoint TTL. The in-memory cache (`cache.MemoryCache`) is built on `patrickmn/go-cache` and runs a background sweep every `CleanupInterval` (default 5 minutes) so unaccessed entries do not linger.
- **Manual.** The refresh header (default `X-Schemads-Refresh`) deletes the specific key for the requesting tenant only — never the whole cache.
- **Capacity.** The default cache keeps at most 4096 entries across response and typed caches. When the limit is exceeded, the oldest entries are evicted.
- **Response size.** Byte-oriented response entries larger than `MaxValueBytes` (default 5 MiB) are not cached. Typed values are still bounded by TTL and `MaxEntries`.

## Column types

Types are aligned with go-mysql-server's type system:

| Category   | Constants                                                                |
| ---------- | ------------------------------------------------------------------------ |
| Boolean    | `boolean`                                                                |
| Integers   | `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64` |
| Float      | `float32`, `float64`                                                     |
| Decimal    | `decimal` (use `Column.Precision` / `Column.Scale`)                      |
| String     | `string`                                                                 |
| Date/Time  | `date`, `datetime`, `timestamp`, `time`, `year`                          |
| Structured | `json`, `enum` (use `Column.Values`), `set` (use `Column.Values`)        |
| Binary     | `blob`, `bit` (use `Column.Size`, 1-64)                                  |

## Operators

Columns declare which filter operations they support via `Column.Operators`:

| Constant                     | Value  |
| ---------------------------- | ------ |
| `OperatorEquals`             | `=`    |
| `OperatorNotEquals`          | `!=`   |
| `OperatorGreaterThan`        | `>`    |
| `OperatorGreaterThanOrEqual` | `>=`   |
| `OperatorLessThan`           | `<`    |
| `OperatorLessThanOrEqual`    | `<=`   |
| `OperatorLike`               | `like` |
| `OperatorIn`                 | `in`   |

## Error handling

Errors are returned as JSON `{"error": "..."}` with an HTTP status:

| Status | Meaning                                           |
| ------ | ------------------------------------------------- |
| `500`  | Handler error or schema validation failure        |
| `501`  | Handler not configured for the requested endpoint |

Each endpoint has its own sentinel error for 501 responses:

| Sentinel                                | Endpoint               |
| --------------------------------------- | ---------------------- |
| `ErrSchemaNotImplemented`               | `fullSchema`           |
| `ErrTablesNotImplemented`               | `tables`               |
| `ErrColumnsNotImplemented`              | `columns`              |
| `ErrTableParameterValuesNotImplemented` | `tableParameterValues` |
| `ErrColumnValuesNotImplemented`         | `columnValues`         |

The `Client` returns these sentinels on 501 so callers can use `errors.Is`:

```go
resp, err := client.FetchColumns(ctx, &schemas.ColumnsRequest{Tables: []string{"t"}})
if errors.Is(err, schemas.ErrColumnsNotImplemented) {
    // plugin does not support this endpoint
}
```

Partial failures can be reported via the `Errors` field on each response type (keyed by table or column name).
