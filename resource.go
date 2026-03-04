package schemas

import (
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const BaseResourcePath = "abstractionSchema"

const (
	// RequestTypeFullSchema returns the complete schema (tables, columns,
	// sub-tables, functions, and sub-table values).
	RequestTypeFullSchema string = "fullSchema"
	// RequestTypeTables returns table names and their sub-table definitions.
	RequestTypeTables string = "tables"
	// RequestTypeColumns returns columns for the tables listed in
	// [SchemaRequest.Tables].
	RequestTypeColumns string = "columns"
	// RequestTypeValues returns possible values for the columns listed in
	// [SchemaRequest.Columns].
	RequestTypeColumnValues string = "columnValues"
	// RequestTypeSubTableValues returns possible values for the sub-tables
	// listed in [SchemaRequest.SubTables].
	RequestTypeTableParameterValues string = "tableParameterValues"
)

func extractHeaders(req *backend.CallResourceRequest) map[string]string {
	headers := make(map[string]string)
	for k, v := range req.Headers {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}
