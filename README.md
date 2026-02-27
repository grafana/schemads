# schemads

> [!CAUTION]
> This repository is experimental and in progress. Do not use this module.

A wrapper for Grafana data source plugins that adds **schema discovery** via `CallResource`. Plugins implement the `SchemaHandler` interface; consumers call the `schema` resource path to retrieve table/column metadata.

## Behaviour

- **Resource path:** `POST` to path `schema` (`schemas.SchemaResourcePath`) with a JSON body containing a `type` field. If the plugin implements `schemas.SchemaHandler`, the wrapper calls it and returns a `SchemaResponse` as JSON. Otherwise it returns **501 Not Implemented**.
- **Other resources:** Any other `CallResource` path is forwarded to the wrapped `CallResourceHandler`. If no next handler is set, returns **404**.

## Usage

### Option A: TableSchemaProvider (recommended)

Implement the `TableSchemaProvider` interface to split schema handling into per-request-type methods. The library handles request routing via `NewSchemaHandlerFromProvider`:

```go
package myplugin

import (
	"context"

	schemas "github.com/grafana/schemads"
)

type MyProvider struct {
	// datasource-specific fields
}

func (p *MyProvider) FullSchema(ctx context.Context) (*schemas.Schema, error) {
	return &schemas.Schema{
		Tables: []schemas.Table{
			{
				Name: "users",
				Columns: []schemas.Column{
					{Name: "id", Type: schemas.ColumnTypeInt64},
					{Name: "name", Type: schemas.ColumnTypeString},
					{Name: "active", Type: schemas.ColumnTypeBoolean, Operators: []schemas.Operator{schemas.OperatorEquals}},
					{Name: "created_at", Type: schemas.ColumnTypeTimestamp},
				},
			},
		},
	}, nil
}

func (p *MyProvider) Tables(ctx context.Context) ([]string, error) {
	return []string{"users", "orders"}, nil
}

func (p *MyProvider) Columns(ctx context.Context, tables []string) (map[string][]schemas.Column, error) {
	// return columns for the requested tables
}

func (p *MyProvider) ColumnValues(ctx context.Context, columns []schemas.ColumnValuesRequest) (map[string][]string, error) {
	// return possible values, or empty map if not supported
	return make(map[string][]string), nil
}

// Convert to SchemaHandler:
// handler := schemas.NewSchemaHandlerFromProvider(&MyProvider{})
```

### Option B: Direct SchemaHandler implementation

For full control, implement `SchemaHandler` directly. Use the `RequestType` constants to switch on `req.Type`:

```go
func (h *MyHandler) Schema(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
	switch req.Type {
	case schemas.RequestTypeFullSchema:
		// return full schema ...
	case schemas.RequestTypeTables:
		return &schemas.SchemaResponse{Tables: []string{"users", "orders"}}, nil
	case schemas.RequestTypeColumns:
		// return columns for req.Tables ...
	case schemas.RequestTypeValues:
		// return values for req.Columns ...
	}
	return nil, fmt.Errorf("unsupported request type: %s", req.Type)
}
```

### Wiring into the plugin

Use `NewSchemaDatasource` to wrap your schema handler. The returned `*SchemaDatasource` implements `backend.CallResourceHandler` — embed it in your instance and delegate `CallResource` to it:

```go
package plugin

import (
	"context"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	schemas "github.com/grafana/schemads"
)

type MyInstance struct {
	*schemas.SchemaDatasource
}

func (m *MyInstance) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return m.SchemaDatasource.CallResource(ctx, req, sender)
}

func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	handler := schemas.NewSchemaHandlerFromProvider(&MyProvider{})

	// Pass an existing CallResourceHandler as the second argument to
	// forward non-schema resource calls, or nil if not needed.
	schemaDs := schemas.NewSchemaDatasource(handler, nil)

	return &MyInstance{SchemaDatasource: schemaDs}, nil
}
```

### Without schema support

Pass `nil` as the schema handler. Requests to the schema path return **501 Not Implemented**; other paths are forwarded to the next handler.

```go
schemaDs := schemas.NewSchemaDatasource(nil, existingCallResourceHandler)
```

## Client-side usage

Consumers that need to fetch schema over HTTP can use `FetchSchema`, which owns the full request serialization and response handling. Both `httpClient` and `schemaURL` are required. Any headers set on `SchemaRequest.Headers` are forwarded to the HTTP request.

```go
resp, err := schemas.FetchSchema(ctx, httpClient, schemaURL, &schemas.SchemaRequest{
	Type:   schemas.RequestTypeColumns,
	Tables: []string{"users"},
})
```

## Request & response format

### Request body

`POST` to `CallResource` with path `schemas.SchemaResourcePath` (`"schema"`):

```json
{
  "type": "columns",
  "tables": ["users", "orders"],
  "columns": []
}
```

| `type`         | Constant                | Description                 | Required fields |
| -------------- | ----------------------- | --------------------------- | --------------- |
| `"fullSchema"` | `RequestTypeFullSchema` | Full schema                 | None            |
| `"tables"`     | `RequestTypeTables`     | List of table names         | None            |
| `"columns"`    | `RequestTypeColumns`    | Columns for specific tables | `tables`        |
| `"values"`     | `RequestTypeValues`     | Values for specific columns | `columns`       |

### Response body

```json
{
  "fullSchema": {
    "tables": [
      {
        "name": "users",
        "columns": [
          { "name": "id", "type": "int64" },
          { "name": "name", "type": "string" },
          { "name": "active", "type": "boolean", "operators": ["=="] },
          { "name": "created_at", "type": "timestamp" }
        ],
        "subTables": []
      }
    ],
    "functions": [],
    "subTableValues": {}
  },
  "tables": ["users", "orders"],
  "columns": {
    "users": [{ "name": "id", "type": "int64" }]
  },
  "columnValues": {},
  "errors": {}
}
```

Which fields are populated depends on `type` in the request.

### Column types

Types are aligned with go-mysql-server's type system:

| Category   | Constants                                                                |
| ---------- | ------------------------------------------------------------------------ |
| Boolean    | `boolean`                                                                |
| Integers   | `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64` |
| Float      | `float32`, `float64`                                                     |
| Decimal    | `decimal` (use `Column.Precision` and `Column.Scale`)                    |
| String     | `string`                                                                 |
| Date/Time  | `date`, `datetime`, `timestamp`, `time`, `year`                          |
| Structured | `json`, `enum` (use `Column.Values`), `set` (use `Column.Values`)        |
| Binary     | `blob`, `bit` (use `Column.Size`)                                        |

### Column metadata

Columns can carry optional metadata:

| Field         | Type         | Purpose                                                       |
| ------------- | ------------ | ------------------------------------------------------------- |
| `operators`   | `[]Operator` | Filter operations the column supports (`==`, `!=`, `>`, etc.) |
| `description` | `string`     | Human/LLM-readable documentation                              |
| `precision`   | `*int`       | Decimal precision                                             |
| `scale`       | `*int`       | Decimal scale                                                 |
| `size`        | `*int`       | Bit width for `bit` type (1-64)                               |
| `values`      | `[]string`   | Allowed members for `enum` and `set` types                    |

### Operators

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

### Response metadata

| Field    | Type                | Purpose                                      |
| -------- | ------------------- | -------------------------------------------- |
| `errors` | `map[string]string` | Per-table/column errors for partial failures |

### Error responses

Errors are returned as JSON with an HTTP status code:

| Status | Meaning                                                                 |
| ------ | ----------------------------------------------------------------------- |
| `400`  | Invalid request (malformed JSON, unknown type, missing required fields) |
| `500`  | Handler returned an error                                               |
| `501`  | No `SchemaHandler` configured                                           |

```json
{ "error": "tables must be specified when requesting columns" }
```
