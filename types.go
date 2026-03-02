// Package schemas provides schema discovery for Grafana data source plugins.
//
// Plugins implement [SchemaHandler] (or the higher-level [TableSchemaProvider])
// and wrap it with [NewSchemaDatasource] to expose table, column, and sub-table
// metadata via CallResource. Consumers fetch metadata with [FetchSchema].
package schemas

import (
	"context"
)

// SchemaHandler is the interface data sources implement to serve schema
// requests. If not configured, requests to [SchemaResourcePath] return
// 501 Not Implemented. See [SchemaHandlerFunc] for an adapter and
// [NewSchemaHandlerFromProvider] for the higher-level alternative.
type SchemaHandler interface {
	Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)
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

type RequestType = string

const (
	// RequestTypeFullSchema returns the complete schema (tables, columns,
	// sub-tables, functions, and sub-table values).
	RequestTypeFullSchema RequestType = "fullSchema"
	// RequestTypeTables returns table names and their sub-table definitions.
	RequestTypeTables RequestType = "tables"
	// RequestTypeColumns returns columns for the tables listed in
	// [SchemaRequest.Tables].
	RequestTypeColumns RequestType = "columns"
	// RequestTypeValues returns possible values for the columns listed in
	// [SchemaRequest.Columns].
	RequestTypeValues RequestType = "values"
	// RequestTypeSubTableValues returns possible values for the sub-tables
	// listed in [SchemaRequest.SubTables].
	RequestTypeSubTableValues RequestType = "subTableValues"
)

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
	Headers   map[string]string       `json:"-"`
	Type      RequestType             `json:"type"`
	Tables    []string                `json:"tables,omitempty"`
	Columns   []ColumnValuesRequest   `json:"columns,omitempty"`
	SubTables []SubTableValuesRequest `json:"subTables,omitempty"`
}

// ColumnValuesRequest identifies a column whose possible values are being
// requested, along with optional parameters to scope the result.
type ColumnValuesRequest struct {
	Table      string            `json:"table"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SubTableValuesRequest identifies a sub-table within a parent table and
// provides dependency context (selected ancestor values) for fetching its
// enumerable values. Table is required because the same sub-table name may
// have different semantics across parent tables.
type SubTableValuesRequest struct {
	Table            string            `json:"table"`
	SubTable         string            `json:"subTable"`
	DependencyValues map[string]string `json:"dependencyValues,omitempty"`
}

// SchemaResponse is the JSON body returned by the schema resource endpoint.
// Which fields are populated depends on the [SchemaRequest.Type] that was sent.
type SchemaResponse struct {
	// FullSchema is populated for "fullSchema" requests.
	FullSchema *Schema `json:"fullSchema,omitempty"`
	// Tables is populated for "tables" requests.
	Tables []string `json:"tables,omitempty"`
	// Columns is populated for "columns" requests (table name -> columns).
	Columns map[string][]Column `json:"columns,omitempty"`
	// ColumnValues is populated for "values" requests (column key -> values).
	ColumnValues map[string][]string `json:"columnValues,omitempty"`
	// SubTables is populated alongside Tables for "tables" requests,
	// mapping table names to their sub-table definitions.
	SubTables map[string][]SubTable `json:"subTables,omitempty"`
	// SubTableValues is populated for "subTableValues" requests.
	SubTableValues map[string][]string `json:"subTableValues,omitempty"`
	// Errors reports per-table or per-column errors for partial failures.
	Errors map[string]string `json:"errors,omitempty"`
}

// Schema is the complete tabular schema returned by a "fullSchema" request.
type Schema struct {
	Tables    []Table  `json:"tables,omitempty"`
	Functions []string `json:"functions,omitempty"`
	// SubTableValues provides pre-populated values for root sub-tables only
	// (table name -> sub-table name -> values). Non-root sub-table values
	// depend on ancestor selections and must be fetched incrementally via
	// a "subTableValues" request with [SubTableValuesRequest.DependencyValues].
	SubTableValues map[string]map[string][]string `json:"subTableValues,omitempty"`
}

// Table describes a single table, its columns, and optional sub-tables.
type Table struct {
	Name      string     `json:"name"`
	SubTables []SubTable `json:"subTables,omitempty"`
	Columns   []Column   `json:"columns,omitempty"`
}

// SubTable describes a hierarchical sub-table within a parent [Table].
// Sub-tables model scoping parameters that must be resolved before the
// table can be queried (e.g. organization -> repository for a GitHub
// data source). Root sub-tables have no dependencies and their values
// may be pre-populated in [Schema.SubTableValues]. Non-root sub-table
// values depend on ancestor selections and must be fetched via a
// "subTableValues" request. See [ValidateSchema] for the full set of
// invariants enforced on sub-table definitions.
type SubTable struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Root      bool     `json:"root"`
	Required  bool     `json:"required,omitempty"`
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
