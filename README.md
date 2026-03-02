# schemads

> [!CAUTION]
> This repository is experimental and in progress. Do not use this module.

A wrapper for Grafana data source plugins that adds **schema discovery** via `CallResource`. Plugins implement `SchemaHandler` (or the higher-level `TableSchemaProvider`); consumers call the `schema` resource path to retrieve table, column, sub-table, and value metadata.

## Overview

- **Resource path:** `POST` to `schema` (`SchemaResourcePath`) with a JSON body containing a `type` field.
- **Schema available:** the wrapper calls the configured `SchemaHandler` and returns a `SchemaResponse` as JSON.
- **Schema not configured:** returns **501 Not Implemented**.
- **Other paths:** forwarded to the wrapped `CallResourceHandler`, or **404** if none is set.

## Plugin integration

### Option A – TableSchemaProvider (recommended)

Implement `TableSchemaProvider` to handle each request type in a separate method. The library routes requests automatically via `NewSchemaHandlerFromProvider`:

```go
type TableSchemaProvider interface {
    FullSchema(ctx context.Context) (*schemas.Schema, error)
    Tables(ctx context.Context) ([]string, map[string][]schemas.SubTable, error)
    Columns(ctx context.Context, tables []string) (map[string][]schemas.Column, error)
    ColumnValues(ctx context.Context, columns []schemas.ColumnValuesRequest) (map[string][]string, error)
    SubTableValues(ctx context.Context, subTables []schemas.SubTableValuesRequest) (map[string][]string, error)
}
```

Example:

```go
type MyProvider struct{}

func (p *MyProvider) FullSchema(ctx context.Context) (*schemas.Schema, error) {
    return &schemas.Schema{
        Tables: []schemas.Table{{
            Name: "issues",
            SubTables: []schemas.SubTable{
                {Name: "organization", Root: true, Required: true},
                {Name: "repository", DependsOn: []string{"organization"}, Required: true},
            },
            Columns: []schemas.Column{
                {Name: "id", Type: schemas.ColumnTypeInt64},
                {Name: "title", Type: schemas.ColumnTypeString},
            },
        }},
    }, nil
}

func (p *MyProvider) Tables(ctx context.Context) ([]string, map[string][]schemas.SubTable, error) {
    return []string{"issues"}, map[string][]schemas.SubTable{
        "issues": {
            {Name: "organization", Root: true, Required: true},
            {Name: "repository", DependsOn: []string{"organization"}, Required: true},
        },
    }, nil
}

func (p *MyProvider) Columns(ctx context.Context, tables []string) (map[string][]schemas.Column, error) {
    // return columns for the requested tables
}

func (p *MyProvider) ColumnValues(ctx context.Context, columns []schemas.ColumnValuesRequest) (map[string][]string, error) {
    return map[string][]string{}, nil
}

func (p *MyProvider) SubTableValues(ctx context.Context, subTables []schemas.SubTableValuesRequest) (map[string][]string, error) {
    result := make(map[string][]string)
    for _, st := range subTables {
        key := st.Table + "_" + st.SubTable
        result[key] = []string{"value1", "value2"}
    }
    return result, nil
}

// Convert to SchemaHandler:
// handler := schemas.NewSchemaHandlerFromProvider(&MyProvider{})
```

### Option B – Direct SchemaHandler

For full control, implement `SchemaHandler` directly:

```go
func (h *MyHandler) Schema(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
    switch req.Type {
    case schemas.RequestTypeFullSchema:
        // return full schema
    case schemas.RequestTypeTables:
        // return tables and sub-tables
    case schemas.RequestTypeColumns:
        // return columns for req.Tables
    case schemas.RequestTypeValues:
        // return values for req.Columns
    case schemas.RequestTypeSubTableValues:
        // return values for req.SubTables
    }
    return nil, fmt.Errorf("unsupported request type: %s", req.Type)
}
```

You can also use `SchemaHandlerFunc` to wrap a plain function:

```go
handler := schemas.SchemaHandlerFunc(func(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
    // ...
})
```

### Wiring into the plugin

`NewSchemaDatasource` returns a `*SchemaDatasource` that implements `backend.CallResourceHandler`:

```go
func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
    handler := schemas.NewSchemaHandlerFromProvider(&MyProvider{})
    // Pass an existing CallResourceHandler as next to forward non-schema
    // resource calls, or nil if not needed.
    schemaDs := schemas.NewSchemaDatasource(handler, nil)
    return &MyInstance{SchemaDatasource: schemaDs}, nil
}
```

### Adding schema support to an existing data source

If your plugin already implements `backend.CallResourceHandler` (e.g. for health checks, autocomplete, or custom endpoints), pass it as the `next` argument. Schema requests are intercepted; everything else is forwarded to your existing handler unchanged:

```go
type MyDatasource struct {
    schemas.SchemaDatasource
}

func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
    ds := &MyDatasource{}

    schemaHandler := schemas.NewSchemaHandlerFromProvider(&MyProvider{})
    // Pass ds as next — any non-schema resource calls continue to
    // be handled by MyDatasource.handleCustomResource.
    ds.SchemaDatasource = *schemas.NewSchemaDatasource(schemaHandler, backend.CallResourceHandlerFunc(ds.handleCustomResource))

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
- `POST /schema` is handled by the schema handler.
- All other paths (e.g. `/custom-endpoint`) are forwarded to `handleCustomResource`.

To opt out of schema support, pass `nil` as the handler — schema requests return 501 and everything else is forwarded:

```go
schemaDs := schemas.NewSchemaDatasource(nil, existingHandler)
```

## Client usage

`FetchSchema` sends a `SchemaRequest` over HTTP and returns the decoded `SchemaResponse`. Both `httpClient` and `schemaURL` are required. Headers on `SchemaRequest.Headers` are forwarded.

```go
resp, err := schemas.FetchSchema(ctx, httpClient, schemaURL, &schemas.SchemaRequest{
    Type:   schemas.RequestTypeTables,
})
// resp.Tables    – []string
// resp.SubTables – map[string][]SubTable
```

Fetching sub-table values incrementally:

```go
resp, err := schemas.FetchSchema(ctx, httpClient, schemaURL, &schemas.SchemaRequest{
    Type: schemas.RequestTypeSubTableValues,
    SubTables: []schemas.SubTableValuesRequest{{
        Table:            "issues",
        SubTable:         "repository",
        DependencyValues: map[string]string{"organization": "grafana"},
    }},
})
// resp.SubTableValues["issues_repository"] – []string
```

## Sub-tables

Sub-tables model hierarchical scoping parameters that consumers resolve before querying a table (e.g. a GitHub data source requires selecting an **organization** and then a **repository** before querying **issues**).

### Definition

Each `SubTable` has:

| Field       | Type       | Description                                                 |
| ----------- | ---------- | ----------------------------------------------------------- |
| `Name`      | `string`   | Unique name within the parent table.                        |
| `DependsOn` | `[]string` | Sibling sub-tables whose values must be selected first.     |
| `Root`      | `bool`     | Entry point with no dependencies. At least one is required. |
| `Required`  | `bool`     | Must be resolved before the table can be queried.           |

### Validation

`ValidateSchema` is called automatically for `fullSchema` responses and enforces:

1. **Unique names** – no two sub-tables in a table share a name.
2. **Valid references** – every `DependsOn` entry names a sibling sub-table.
3. **Acyclic graph** – the dependency graph has no cycles.
4. **Root rules** – at least one sub-table is `Root` and root sub-tables have no dependencies.
5. **Required chain** – a required sub-table may only depend on other required sub-tables.
6. **SubTableValues integrity** – `Schema.SubTableValues` keys reference existing tables and root sub-tables only. Non-root sub-table values depend on ancestor selections and must be fetched incrementally via a `"subTableValues"` request with `DependencyValues`.

Call `ValidateSchema` directly to validate schemas you construct manually.

### Composite keys

Sub-table values in `SchemaResponse.SubTableValues` use composite keys of the form `table_subTable` — e.g. `"issues_organization"`.

> **Note:** table and sub-table names may contain underscores, making this separator ambiguous. A future version will adopt a safer delimiter once the protocol is versioned.

## Request types

| `type`             | Constant                    | Required fields | Response fields       |
| ------------------ | --------------------------- | --------------- | --------------------- |
| `"fullSchema"`     | `RequestTypeFullSchema`     | –               | `FullSchema`          |
| `"tables"`         | `RequestTypeTables`         | –               | `Tables`, `SubTables` |
| `"columns"`        | `RequestTypeColumns`        | `Tables`        | `Columns`             |
| `"values"`         | `RequestTypeValues`         | `Columns`       | `ColumnValues`        |
| `"subTableValues"` | `RequestTypeSubTableValues` | `SubTables`     | `SubTableValues`      |

`ValidateRequest` is called automatically before dispatching and rejects unknown types or missing required fields.

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
| `OperatorEquals`             | `==`   |
| `OperatorNotEquals`          | `!=`   |
| `OperatorGreaterThan`        | `>`    |
| `OperatorGreaterThanOrEqual` | `>=`   |
| `OperatorLessThan`           | `<`    |
| `OperatorLessThanOrEqual`    | `<=`   |
| `OperatorLike`               | `like` |
| `OperatorIn`                 | `in`   |

## Error handling

Errors are returned as JSON `{"error": "..."}` with an HTTP status:

| Status | Meaning                                             |
| ------ | --------------------------------------------------- |
| `400`  | Invalid request (unknown type, missing fields, etc) |
| `500`  | Handler error or schema validation failure          |
| `501`  | No `SchemaHandler` configured                       |

Partial failures can be reported via `SchemaResponse.Errors` (keyed by table or column name).
