package schemas

import (
	"context"
)

// SchemaHandler is the interface data sources implement to provide tabular
// information (full schema, tables, columns, column values). If not implemented,
// schema resource requests return 501 Not Implemented.
type SchemaHandler interface {
	Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error)
}

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
	RequestTypeFullSchema RequestType = "fullSchema"
	RequestTypeTables     RequestType = "tables"
	RequestTypeColumns    RequestType = "columns"
	RequestTypeValues     RequestType = "values"
)

// SchemaRequest is the request for tabular information.
// Columns request requires Tables to be set; values request requires Columns to be set.
type SchemaRequest struct {
	Headers map[string]string     `json:"-"`
	Type    RequestType           `json:"type"`
	Tables  []string              `json:"tables,omitempty"`
	Columns []ColumnValuesRequest `json:"columns,omitempty"`
}

// ColumnValuesRequest identifies a column (and optional parameters) when requesting values.
type ColumnValuesRequest struct {
	Table      string            `json:"table"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SchemaResponse is the response containing schema/tables/columns/values.
type SchemaResponse struct {
	FullSchema   Schema              `json:"fullSchema,omitempty"`
	Tables       []string            `json:"tables,omitempty"`
	Columns      map[string][]Column `json:"columns,omitempty"`
	ColumnValues map[string][]string `json:"columnValues,omitempty"`

	// Errors reports per-table or per-column errors for partial failures.
	// Keys match the table or column name that failed; values describe the error.
	Errors map[string]string `json:"errors,omitempty"`
}

// Schema is the full tabular schema.
type Schema struct {
	Tables         []Table                        `json:"tables,omitempty"`
	Functions      []string                       `json:"functions,omitempty"`
	SubTableValues map[string]map[string][]string `json:"subTableValues,omitempty"`
}

// Table represents a table with optional sub-tables and columns.
type Table struct {
	Name      string     `json:"name"`
	SubTables []SubTable `json:"subTables,omitempty"`
	Columns   []Column   `json:"columns,omitempty"`
}

// SubTable represents a sub-table (e.g. for hierarchical enumeration).
type SubTable struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Root      bool     `json:"root,omitempty"`
}

// Operator represents a filter operation that a column supports.
// Consumers use this to decide which SQL index operations can be
// pushed down to the datasource.
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

// Column describes a single column in a table.
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

// ColumnType is the scalar type of a column.
// Values are aligned with go-mysql-server's type system so consumers
// can map them directly to SQL types without loss of fidelity.
type ColumnType string

const (
	ColumnTypeString   ColumnType = "string"
	ColumnTypeDatetime ColumnType = "datetime"
	ColumnTypeBoolean  ColumnType = "boolean"

	// Sized signed integers.
	ColumnTypeInt8  ColumnType = "int8"
	ColumnTypeInt16 ColumnType = "int16"
	ColumnTypeInt32 ColumnType = "int32"
	ColumnTypeInt64 ColumnType = "int64"

	// Sized unsigned integers.
	ColumnTypeUint8  ColumnType = "uint8"
	ColumnTypeUint16 ColumnType = "uint16"
	ColumnTypeUint32 ColumnType = "uint32"
	ColumnTypeUint64 ColumnType = "uint64"

	// Floating point.
	ColumnTypeFloat32 ColumnType = "float32"
	ColumnTypeFloat64 ColumnType = "float64"

	// Arbitrary-precision decimal. Use Column.Precision and Column.Scale
	// to specify the decimal parameters.
	ColumnTypeDecimal ColumnType = "decimal"

	// Date and time types.
	ColumnTypeDate      ColumnType = "date"
	ColumnTypeTimestamp ColumnType = "timestamp"
	ColumnTypeTime      ColumnType = "time"
	ColumnTypeYear      ColumnType = "year"

	// Structured types. ColumnTypeEnum and ColumnTypeSet require
	// Column.Values to list the allowed members.
	ColumnTypeJSON ColumnType = "json"
	ColumnTypeEnum ColumnType = "enum"
	ColumnTypeSet  ColumnType = "set"

	// Binary types. ColumnTypeBit uses Column.Size for the bit width (1-64).
	ColumnTypeBlob ColumnType = "blob"
	ColumnTypeBit  ColumnType = "bit"
)
