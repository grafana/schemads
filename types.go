package schemas

import (
	"context"
	"fmt"
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
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}
	return f.fn(ctx, req)
}

// SchemaRequest is the request for tabular information.
// Type may be "" (full schema), "tables", "columns", or "values".
// Columns request requires Tables to be set; values request requires Columns to be set.
type SchemaRequest struct {
	Headers map[string]string
	Type    string // "", "tables", "columns", "values"
	Tables  []string
	Columns []ColumnsInformationRequest
}

// ColumnsInformationRequest identifies a column (and optional parameters) when requesting values.
type ColumnsInformationRequest struct {
	Table      string
	Parameters map[string]string
}

// SchemaResponse is the response containing schema/tables/columns/values.
type SchemaResponse struct {
	FullSchema   Schema
	Tables       []string
	Columns      map[string][]Column
	ColumnValues map[string][]string
}

// Schema is the full tabular schema.
type Schema struct {
	Tables         []Table
	Functions      []string
	SubTableValues map[string]map[string][]string
}

// Table represents a table with optional sub-tables and columns.
type Table struct {
	Name      string
	SubTables []SubTable
	Columns   []Column
}

// SubTable represents a sub-table (e.g. for hierarchical enumeration).
type SubTable struct {
	Name      string
	DependsOn []string
	Root      bool
}

// Column is a column name and type.
type Column struct {
	Name string
	Type ColumnType
}

// ColumnType is the scalar type of a column.
type ColumnType string

const (
	ColumnTypeNumber   ColumnType = "number"
	ColumnTypeString   ColumnType = "string"
	ColumnTypeDatetime ColumnType = "datetime"
)

// ValidateRequest validates a TableInformationRequest (type and required fields).
func ValidateRequest(req *SchemaRequest) error {
	if req == nil {
		return nil
	}
	if req.Type != "" && req.Type != "tables" && req.Type != "columns" && req.Type != "values" {
		return fmt.Errorf("invalid table information request type: must be one of tables, columns, values")
	}
	if req.Type == "columns" && len(req.Tables) == 0 {
		return fmt.Errorf("tables must be specified when requesting columns")
	}
	if req.Type == "values" {
		if len(req.Columns) == 0 {
			return fmt.Errorf("columns must be specified when requesting values")
		}
	}
	return nil
}
