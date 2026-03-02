# schemads

> [!CAUTION]
> This repository is experimental and in progress. Do not use this module.

A wrapper for Grafana data source plugins that adds **schema discovery** via `CallResource`. Plugins implement the `SchemaHandler` interface; consumers call the `schema` resource path to retrieve table/column metadata.

## Behaviour

- **Resource path:** `POST` to path `schema` (`schemas.SchemaResourcePath`) with a JSON body. If the plugin implements `schemas.SchemaHandler`, the wrapper calls it and returns a `SchemaResponse` as JSON. Otherwise it returns **501 Not Implemented**.
- **Other resources:** Any other `CallResource` path is forwarded to the wrapped `CallResourceHandler`. If no next handler is set, returns **404**.

## Usage

### 1. Implement `SchemaHandler`

Create a type that satisfies the `SchemaHandler` interface. The `Schema` method receives a `SchemaRequest` and should return the appropriate response based on `req.Type`:

```go
package myplugin

import (
	"context"

	schemas "github.com/grafana/schemads"
)

type MySchemaHandler struct {
	// datasource-specific fields (client, config, etc.)
}

func (h *MySchemaHandler) Schema(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
	switch req.Type {
	case "tables":
		return &schemas.SchemaResponse{
			Tables: []string{"users", "orders"},
		}, nil

	case "columns":
		columns := make(map[string][]schemas.Column)
		for _, table := range req.Tables {
			columns[table] = getColumnsForTable(table)
		}
		return &schemas.SchemaResponse{Columns: columns}, nil

	case "values":
		return &schemas.SchemaResponse{
			ColumnValues: make(map[string][]string),
		}, nil

	default: // full schema
		return &schemas.SchemaResponse{
			FullSchema: &schemas.Schema{
				Tables: []schemas.Table{
					{
						Name: "users",
						Columns: []schemas.Column{
							{Name: "id", Type: schemas.ColumnTypeNumber},
							{Name: "name", Type: schemas.ColumnTypeString},
							{Name: "created_at", Type: schemas.ColumnTypeDatetime},
						},
					},
				},
			},
		}, nil
	}
}
```

### 2. Wire into the plugin

Use `NewSchemaDatasource` to wrap your schema handler. The returned `*SchemaDatasource` implements `backend.CallResourceHandler` — embed it in your instance and delegate `CallResource` to it:

```go
package plugin

import (
	"context"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	schemas "github.com/grafana/schemads"
)

type MyInstance struct {
	// your datasource fields
	*schemas.SchemaDatasource
}

func (m *MyInstance) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	return m.SchemaDatasource.CallResource(ctx, req, sender)
}

func NewInstance(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	ds := createMyDatasource(ctx, settings) // your plugin-specific setup
	handler := &MySchemaHandler{/* ... */}

	// Pass an existing CallResourceHandler as the second argument to
	// forward non-schema resource calls, or nil if not needed.
	schemaDs := schemas.NewSchemaDatasource(handler, nil)

	return &MyInstance{
		SchemaDatasource: schemaDs,
		// ...
	}, nil
}
```

### Without schema support

Pass `nil` as the schema handler. Requests to the schema path return **501 Not Implemented**; other paths are forwarded to the next handler.

```go
schemaDs := schemas.NewSchemaDatasource(nil, existingCallResourceHandler)
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

| `type`            | Description                 | Required fields |
| ----------------- | --------------------------- | --------------- |
| `""` (or omitted) | Full schema                 | None            |
| `"tables"`        | List of table names         | None            |
| `"columns"`       | Columns for specific tables | `tables`        |
| `"values"`        | Values for specific columns | `columns`       |

### Response body

```json
{
  "fullSchema": {
    "tables": [
      {
        "name": "users",
        "columns": [
          { "name": "id", "type": "number" },
          { "name": "name", "type": "string" },
          { "name": "created_at", "type": "datetime" }
        ],
        "subTables": []
      }
    ],
    "functions": [],
    "subTableValues": {}
  },
  "tables": ["users", "orders"],
  "columns": {
    "users": [{ "name": "id", "type": "number" }]
  },
  "columnValues": {}
}
```

Which fields are populated depends on `type` in the request. Column types are one of `"number"`, `"string"`, or `"datetime"`.

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
