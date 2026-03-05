package schemas

import (
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const BaseResourcePath = "abstractionSchema"

const (
	// RequestTypeFullSchema returns the complete schema (tables, columns,
	// table parameters, functions, and table parameter values).
	RequestTypeFullSchema string = "fullSchema"
	// RequestTypeTables returns table names and their table parameter definitions.
	RequestTypeTables string = "tables"
	// RequestTypeColumns returns columns for the tables listed in
	// [ColumnsRequest.Tables].
	RequestTypeColumns string = "columns"
	// RequestTypeColumnValues returns possible values for the columns listed in
	// [ColumnValuesRequest.Columns].
	RequestTypeColumnValues string = "columnValues"
	// RequestTypeTableParameterValues returns possible values for table parameters
	// identified by [TableParameterValuesRequest].
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
