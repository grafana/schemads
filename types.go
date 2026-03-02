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

// SchemaRequest is the request for tabular information.
// Type may be "" (full schema), "tables", "columns", or "values".
// Columns request requires Tables to be set; values request requires Columns to be set.
type SchemaRequest struct {
	Headers map[string]string           `json:"-"`
	Type    string                      `json:"type"`
	Tables  []string                    `json:"tables,omitempty"`
	Columns []ColumnsInformationRequest `json:"columns,omitempty"`
}

// ColumnsInformationRequest identifies a column (and optional parameters) when requesting values.
type ColumnsInformationRequest struct {
	Table      string            `json:"table"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SchemaResponse is the response containing schema/tables/columns/values.
type SchemaResponse struct {
	FullSchema   *Schema             `json:"fullSchema,omitempty"`
	Tables       []string            `json:"tables,omitempty"`
	Columns      map[string][]Column `json:"columns,omitempty"`
	ColumnValues map[string][]string `json:"columnValues,omitempty"`
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
	Root      bool     `json:"root"`
}

// Column is a column name and type.
type Column struct {
	Name string     `json:"name"`
	Type ColumnType `json:"type"`
}

// ColumnType is the scalar type of a column.
type ColumnType string

const (
	ColumnTypeNumber   ColumnType = "number"
	ColumnTypeString   ColumnType = "string"
	ColumnTypeDatetime ColumnType = "datetime"
)
