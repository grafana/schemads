# grafana-data-source-schemas

A wrapper for Grafana data sources that adds support for requesting **schemas** (full schema, tables, columns, and column values) via resource calls.

## Behaviour

- **Resource path:** `POST` to path `schema` (see `schemas.SchemaResourcePath`) with a JSON body (see below). If the data source implements `schemas.SchemaHandler`, the wrapper calls it and returns a `SchemaResponse` as JSON. Otherwise it returns **501 Not Implemented**.
- **Other resources:** Any other `CallResource` path is forwarded to the wrapped `CallResourceHandler`.

## Usage

### With a data source that implements schema (e.g. SQL)

```go
package main

import (
	"context"
	"log"
	"os"

	schemas "github.com/grafana/schemads"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/sqlds"
)

func main() {
	if err := datasource.Manage("my-datasource", newInstanceManager(), datasource.ManageOpts{}); err != nil {
		log.Printf("Error: %s", err)
		os.Exit(1)
	}
}

func newInstanceManager() datasource.InstanceFactoryFunc {
	return func(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
		driver := &myDriver{}
		base := sqlds.NewDatasource(driver)
		instance, err := base.NewDatasource(ctx, settings)
		if err != nil {
			return nil, err
		}
		// Wrap the base instance: schema handler + next CallResourceHandler
		wrapped := schemas.NewSchemaDatasource(driver, instance.(backend.CallResourceHandler))
		return wrapped.NewDatasource(ctx, settings)
	}
}

type myDriver struct {
	sqlds.Driver
}

func (d *myDriver) Schema(ctx context.Context, req *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
	// Implement based on req.Type: "", "tables", "columns", "values"
	return &schemas.SchemaResponse{Tables: []string{"users", "orders"}}, nil
}
```

### With a data source that does **not** implement schema

Pass `nil` as the schema handler. Requests to the schema path return **501 Not Implemented**; other paths are forwarded to the wrapped handler.

```go
wrapped := schemas.NewSchemaDatasource(nil, baseCallResourceHandler)
return wrapped.NewDatasource(ctx, settings)
```

### Resource request body (schema path)

JSON body for `CallResource` with path `schemas.SchemaResourcePath` (`"schema"`):

```json
{
  "type": "",
  "tables": [],
  "columns": []
}
```

- **Full schema:** `"type": ""` or omit. No `tables`/`columns` required.
- **Tables only:** `"type": "tables"`.
- **Columns for tables:** `"type": "columns"`, `"tables": ["table1", "table2"]`.
- **Values for columns:** `"type": "values"`, `"columns": [{"table": "t1", "parameters": {}}]`.

Response is a **SchemaResponse** (JSON): `FullSchema`, `Tables`, `Columns`, `ColumnValues`.
