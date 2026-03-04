// Package schemas provides schema discovery for Grafana data source plugins.
//
// Plugins implement [SchemaHandler] (or the higher-level [TableSchemaProvider])
// and wrap it with [NewSchemaDatasource] to expose table, column, and sub-table
// metadata via CallResource. Consumers fetch metadata with [FetchSchema].
package schemas

import (
	"context"

	apidata "github.com/grafana/grafana-plugin-sdk-go/experimental/apis/datasource/v0alpha1"
)

// SchemaHandler is the interface data sources implement to serve schema
// requests. If not configured, requests to [SchemaResourcePath] return
// 501 Not Implemented. See [SchemaHandlerFunc] for an adapter and
// [NewSchemaHandlerFromProvider] for the higher-level alternative.
type SchemaHandler interface {
	Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)
}

type TablesHandler interface {
	Tables(ctx context.Context, req *TablesRequest) (*TablesResponse, error)
}

type ColumnsHandler interface {
	Columns(ctx context.Context, req *ColumnsRequest) (*ColumnsResponse, error)
}

type TableParameterValuesHandler interface {
	TableParameterValues(ctx context.Context, req *TableParameterValuesRequest) (*TableParametersValuesResponse, error)
}

type ColumnValuesHandler interface {
	ColumnValues(ctx context.Context, req *ColumnValuesRequest) (*ColumnValuesResponse, error)
}

// SchemaHandlerFunc adapts an ordinary function into a [SchemaHandler].
func SchemaHandlerFunc(fn func(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)) SchemaHandler {
	return &schemaHandlerFunc{fn: fn}
}

type schemaHandlerFunc struct {
	fn func(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)
}

func (f *schemaHandlerFunc) Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
	return f.fn(ctx, req)
}

// SubTableSeparator is used to build composite keys that reference a
// sub-table within a parent table (e.g. "issues_organization").
//
// NOTE: sub-table and table names may themselves contain underscores,
// making this separator ambiguous. A future version will adopt a safer
// delimiter (e.g. "/" or a structured identifier) once the protocol is
// versioned.
const SubTableSeparator = "_"

// SchemaRequest is the JSON body sent to the schema resource endpoint.
// Which fields are required depends on [SchemaRequest.Type]:
//
//   - "columns" requires [SchemaRequest.Tables].
//   - "values" requires [SchemaRequest.Columns].
//   - "subTableValues" requires [SchemaRequest.SubTables].
type SchemaRequest struct {
	Headers map[string]string `json:"-"`
}

type TablesRequest struct {
	Headers map[string]string `json:"-"`
}

type ColumnsRequest struct {
	Headers map[string]string `json:"-"`
	Tables  []string          `json:"tables"`
}

// ColumnValuesRequest identifies a column whose possible values are being
// requested, along with optional parameters to scope the result.
type ColumnValuesRequest struct {
	Headers         map[string]string `json:"-"`
	Table           string            `json:"table"`
	Columns         []string          `json:"columns,omitempty"`
	TableParameters map[string]string `json:"tableParameters,omitempty"`
	TimeRange       apidata.TimeRange `json:"timeRange"`
}

// TableParameterValuesRequest identifies a table and
// provides dependency context (selected ancestor values) for fetching its
// enumerable values. Table is required because the same table parameter name may
// have different semantics across parent tables.
type TableParameterValuesRequest struct {
	Headers          map[string]string `json:"-"`
	Table            string            `json:"table"`
	TableParameter   string            `json:"tableParameter,omitempty"`
	DependencyValues map[string]string `json:"dependencyValues,omitempty"`
}

// SchemaResponse is the JSON body returned by the schema resource endpoint.
// Which fields are populated depends on the [SchemaRequest.Type] that was sent.
type SchemaResponse struct {
	// FullSchema is populated for "fullSchema" requests.
	FullSchema *Schema `json:"fullSchema,omitempty"`
	Errors     string  `json:"error,omitempty"`
}

type TablesResponse struct {
	Tables          []string                    `json:"tables"`
	TableParameters map[string][]TableParameter `json:"tableParameters,omitempty"`
	Errors          map[string]string           `json:"errors,omitempty"`
}

type ColumnsResponse struct {
	Columns map[string][]Column `json:"columns"`
	Errors  map[string]string   `json:"errors,omitempty"`
}

type ColumnValuesResponse struct {
	ColumnValues map[string][]string `json:"columnValues"`
	Errors       map[string]string   `json:"errors,omitempty"`
}

type TableParametersValuesResponse struct {
	TableParameterValues map[string][]string `json:"tableParameterValues"`
	Errors               map[string]string   `json:"errors,omitempty"`
}

// Schema is the complete tabular schema returned by a "fullSchema" request.
type Schema struct {
	Tables    []Table  `json:"tables,omitempty"`
	Functions []string `json:"functions,omitempty"`
	// TableParameterValues provides pre-populated values for root table parameters only
	// (table name -> table parameter name -> values). Non-root table parameter values
	// depend on ancestor selections and must be fetched incrementally via
	// a "tableParameterValues" request with [TableParameterValuesRequest.DependencyValues].
	TableParameterValues map[string]map[string][]string `json:"tableParameterValues,omitempty"`
}

// Table describes a single table, its columns, and optional table parameters.
type Table struct {
	Name            string           `json:"name"`
	TableParameters []TableParameter `json:"tableParameters,omitempty"`
	Columns         []Column         `json:"columns,omitempty"`
}

// TableParameter describes a hierarchical table parameter within a parent [Table].
// Table parameters model scoping parameters that must be resolved before the
// table can be queried (e.g. organization -> repository for a GitHub
// data source). Root table parameters have no dependencies and their values
// may be pre-populated in [Schema.TableParameterValues]. Non-root table
// parameter values depend on ancestor selections and must be fetched via a
// "tableParameterValues" request. See [ValidateSchema] for the full set of
// invariants enforced on table parameter definitions.
type TableParameter struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Root      bool     `json:"root"`
	Required  bool     `json:"required"`
}

// Operator represents a filter operation that a column supports.
// Consumers use this to decide which filter predicates can be pushed
// down to the data source.
type Operator string

const (
	OperatorEquals             Operator = "=="
	OperatorNotEquals          Operator = "!="
	OperatorGreaterThan        Operator = ">"
	OperatorGreaterThanOrEqual Operator = ">="
	OperatorLessThan           Operator = "<"
	OperatorLessThanOrEqual    Operator = "<="
	OperatorLike               Operator = "like"
	OperatorIn                 Operator = "in"
)

// Column describes a single column in a table, including its type and
// optional metadata such as supported filter operators and documentation.
type Column struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
	// Operators lists the filter operations this column supports.
	// When empty, consumers should assume no filtering is available
	// and avoid pushing filters for this column.
	Operators []Operator `json:"operators,omitempty"`
	// Description is optional human/LLM-readable documentation for the column.
	Description string `json:"description,omitempty"`
	// Precision and Scale apply to ColumnTypeDecimal.
	Precision *int `json:"precision,omitempty"`
	Scale     *int `json:"scale,omitempty"`
	// Size applies to ColumnTypeBit (bit width, 1-64).
	Size *int `json:"size,omitempty"`
	// Values lists the allowed members for ColumnTypeEnum and ColumnTypeSet (if available).
	Values []string `json:"values,omitempty"`
}

// ColumnType is the scalar type of a column. Values are aligned with
// go-mysql-server's type system so consumers can map them directly to
// SQL types without loss of fidelity.
type ColumnType string

const (
	ColumnTypeString   ColumnType = "string"
	ColumnTypeDatetime ColumnType = "datetime"
	ColumnTypeBoolean  ColumnType = "boolean"

	ColumnTypeInt8  ColumnType = "int8"
	ColumnTypeInt16 ColumnType = "int16"
	ColumnTypeInt32 ColumnType = "int32"
	ColumnTypeInt64 ColumnType = "int64"

	ColumnTypeUint8  ColumnType = "uint8"
	ColumnTypeUint16 ColumnType = "uint16"
	ColumnTypeUint32 ColumnType = "uint32"
	ColumnTypeUint64 ColumnType = "uint64"

	ColumnTypeFloat32 ColumnType = "float32"
	ColumnTypeFloat64 ColumnType = "float64"

	// ColumnTypeDecimal requires Column.Precision and Column.Scale.
	ColumnTypeDecimal ColumnType = "decimal"

	ColumnTypeDate      ColumnType = "date"
	ColumnTypeTimestamp ColumnType = "timestamp"
	ColumnTypeTime      ColumnType = "time"
	ColumnTypeYear      ColumnType = "year"

	// ColumnTypeEnum and ColumnTypeSet require Column.Values.
	ColumnTypeJSON ColumnType = "json"
	ColumnTypeEnum ColumnType = "enum"
	ColumnTypeSet  ColumnType = "set"

	// ColumnTypeBit requires Column.Size (1-64).
	ColumnTypeBlob ColumnType = "blob"
	ColumnTypeBit  ColumnType = "bit"
)
