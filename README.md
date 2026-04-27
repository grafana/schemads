# schemads

> [!CAUTION]
> This repository is experimental and in progress. Do not use this module.

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

### Table references (`tables`)

Consumers that need to embed a parameterised table in a single human-writable string (for example, an internal query language that says `FROM <ref>`) can use the `tables` subpackage.

A reference is an undelimited identifier of the form:

```text
<table>(<param1>=<value1>,<param2>=<value2>,...)
```

Examples:

```text
events                                   // no parameters
events(env=prod)                         // one parameter
events(env=prod,service=tempo)           // multiple parameters
tags(name=Promo \(2024\))                // value with escaped reserved chars
```

The format is purely syntactic — `tables` does not produce or consume SQL. It exists so that a reference can be parsed unambiguously back into `(table, map[paramName]paramValue)` even when parameter values are user-supplied free text.

The encoded form has no outer delimiters. If the surrounding system wraps references in delimiters of its own (for example, backticks in a query language), callers must add them on the way out and strip them before calling `Parse`.

**Reserved characters.** Inside a table name, parameter key, or parameter value the five characters `(`, `)`, `,`, `=`, and `\` are reserved and must be backslash-escaped (`\(`, `\,`, `\\`, etc.). Any other backslash sequence is a parse error. Backticks are not reserved and are passed through verbatim.

**Whitespace.** Decoding tolerates optional whitespace around `(`, `)`, `=`, and `,`, so `events(env=prod, service=tempo)` and `events(env=prod,service=tempo)` parse identically. Whitespace inside a value is preserved verbatim, but leading and trailing ASCII whitespace in a value is treated as separator padding and is not preserved across round-trips.

**Empty values.** `events(env=)` decodes to `{"env": ""}` (an empty string). A key absent from the parameter list is unset, which is distinct from an empty string.

**API.**

```go
import "github.com/grafana/schemads/tables"

ref, err := tables.Parse("events(env=prod,service=tempo)")
// ref.Table       -> "events"
// ref.TableParams -> map[string]string{"env": "prod", "service": "tempo"}

s := tables.TableRef{
    Table:       "events",
    TableParams: map[string]string{"env": "prod", "service": "tempo"},
}.String()
// s -> "events(env=prod,service=tempo)"   (params sorted, escaped)

if err := tables.Validate(ref, schema); err != nil {
    // ErrUnknownTable, ErrUnknownParameter, or ErrMissingRequired
}
```

`Parse` performs only syntactic validation. Use `Validate` to check a decoded reference against a `Schema`: that the table exists, every key is a declared parameter, and every required parameter is present. See the package documentation for the full grammar.

#### Legacy underscore form (best-effort fallback)

Earlier consumers encoded a parameterised table as positional values appended to the table name with `_`, for example `issues_grafana_loki` for `(table=issues, organization=grafana, repository=loki)`. That form is fundamentally ambiguous — table names and values may both contain `_`, and parameter names are not encoded — and is being phased out in favour of the canonical form above.

To migrate callers gradually, `tables` exposes two helpers that accept the legacy form on a best-effort, schema-aware basis:

```go
// Strict legacy decoder. Uses schema to find the longest matching table
// name and bind trailing "_"-separated fields positionally to the table's
// declared parameters.
ref, err := tables.ParseLegacy("issues_grafana_loki", schema)

// Dispatches by syntax:
//   - input contains an unescaped "("  -> Parse only (canonical with params)
//   - input has no unescaped "("        -> ParseLegacy first, then Parse
// Joins both errors via errors.Join when the no-paren branch fails on both.
ref, err := tables.ParseWithFallback(input, schema)
```

`ParseLegacy` will return `ErrSyntax` when no table in the schema matches the input under the positional-value rule. Parameter values containing `_` cannot be recovered correctly; new code should emit only the canonical form.

The unescaped-paren heuristic is purely syntactic — `ParseWithFallback` does not inspect the schema for the canonical case. An input like `events(env=prod)` is always routed to `Parse`, and an input like `issues_grafana_loki` is always routed to `ParseLegacy` first. Callers that know which producer emitted an input should call `Parse` or `ParseLegacy` directly to skip the routing.

## Endpoints

| Path                   | Constant                          | Request type                  | Response type                   |
| ---------------------- | --------------------------------- | ----------------------------- | ------------------------------- |
| `fullSchema`           | `RequestTypeFullSchema`           | `SchemaRequest`               | `SchemaResponse`                |
| `tables`               | `RequestTypeTables`               | `TablesRequest`               | `TablesResponse`                |
| `columns`              | `RequestTypeColumns`              | `ColumnsRequest`              | `ColumnsResponse`               |
| `columnValues`         | `RequestTypeColumnValues`         | `ColumnValuesRequest`         | `ColumnValuesResponse`          |
| `tableParameterValues` | `RequestTypeTableParameterValues` | `TableParameterValuesRequest` | `TableParametersValuesResponse` |

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
