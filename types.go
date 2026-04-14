// Package schemas provides schema discovery for Grafana data source plugins.
//
// Plugins implement handler interfaces ([SchemaHandler], [TablesHandler],
// [ColumnsHandler], etc.) and wire them into [NewSchemaDatasource] to expose
// table, column, and table parameter metadata via CallResource. Consumers fetch
// metadata with [Client].
package schemas

import (
	"context"
	"net/http"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	apidata "github.com/grafana/grafana-plugin-sdk-go/experimental/apis/datasource/v0alpha1"
)

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

type CommonRequest struct {
	Headers       http.Header           `json:"-"`
	PluginContext backend.PluginContext `json:"-"`
}

type SchemaRequest struct {
	CommonRequest
}

type TablesRequest struct {
	CommonRequest
}

type ColumnsRequest struct {
	CommonRequest
	Tables          []string          `json:"tables"`
	TableParameters map[string]string `json:"tableParameters,omitempty"`
}

// ColumnValuesRequest identifies a column whose possible values are being
// requested, along with optional parameters to scope the result.
type ColumnValuesRequest struct {
	CommonRequest
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
	CommonRequest
	Table            string            `json:"table"`
	TableParameter   string            `json:"tableParameter,omitempty"`
	DependencyValues map[string]string `json:"dependencyValues,omitempty"`
}

// SchemaResponse is the JSON body returned by the fullSchema endpoint.
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
	// TableHints lists the per-table execution hints this table supports
	// via FOR (...) clauses in SQL.
	TableHints []TableHint `json:"tableHints,omitempty"`
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
	OperatorEquals             Operator = "="
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

// Generic abstraction types

// FilterCondition represents a single filter condition applied to a column.
// For scalar operators (=, !=, >, >=, <, <=, like) use Value.
// For the "in" operator use Values.
type FilterCondition struct {
	Operator Operator `json:"operator"`
	Value    any      `json:"value,omitempty"`
	Values   []any    `json:"values,omitempty"`
}

// ColumnFilter represents a set of filters applied to a specific column.
// Multiple conditions are ANDed together.
type ColumnFilter struct {
	Name       string            `json:"name"`
	Conditions []FilterCondition `json:"conditions"`
}

<<<<<<< kb/datasource-hints
// TableHint describes a per-table execution hint that a datasource supports.
// Hints are specified via FOR (...) clauses in SQL and control how the
// datasource backend executes the query for a specific table — e.g.
// rate('5m'), step('30s'), instant. Unlike table parameters, hints do not
// appear as columns and do not affect the table schema.
type TableHint struct {
	// Name is the hint identifier used in SQL: FOR (name('value')).
	Name string `json:"name"`
	// Description is optional human/LLM-readable documentation.
	Description string `json:"description,omitempty"`
	// HasValue indicates whether the hint takes a string argument.
	// If false, the hint is a flag: FOR (instant). If true: FOR (rate('5m')).
	HasValue bool `json:"hasValue,omitempty"`
=======
// OrderByColumn specifies a column and sort direction for ORDER BY pushdown.
type OrderByColumn struct {
	Name string `json:"name"`
	Desc bool   `json:"desc,omitempty"`
>>>>>>> main
}

type Query struct {
	apidata.CommonQueryProperties `json:",inline"`
<<<<<<< kb/datasource-hints
	Table                         string            `json:"table"`
	Filters                       []ColumnFilter    `json:"filters"`
	TableParameterValues          map[string]any    `json:"tableParameterValues,omitempty"`
	// TableHintValues carries the per-table hints from FOR (...) clauses.
	// Keys are uppercase hint names, values are the hint arguments (empty for flags).
	TableHintValues               map[string]string `json:"tableHintValues,omitempty"`
	GrafanaSql                    bool              `json:"grafanaSql"`
=======
	Table                         string         `json:"table"`
	Filters                       []ColumnFilter `json:"filters"`
	TableParameterValues          map[string]any `json:"tableParameterValues,omitempty"`
	GrafanaSql                    bool           `json:"grafanaSql"`

	// Pushdown hints — datasources MAY use these to optimize queries.
	// The SQL engine still applies these operations on the result for correctness,
	// so datasources that ignore them produce correct (but potentially slower) results.
	Columns []string        `json:"columns,omitempty"` // SELECT column projection (nil = all columns)
	OrderBy []OrderByColumn `json:"orderBy,omitempty"`
	Limit   *int64          `json:"limit,omitempty"`
>>>>>>> main
}
