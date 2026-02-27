package schemas

import (
	"context"
	"fmt"
)

// TableSchemaProvider is a higher-level interface that splits schema handling
// into per-request-type methods. Use NewSchemaHandlerFromProvider to convert
// a TableSchemaProvider into a SchemaHandler with automatic request routing.
type TableSchemaProvider interface {
	// FullSchema returns the complete schema including all tables, columns,
	// sub-tables, functions, and sub-table values.
	// Column values are not included.
	FullSchema(ctx context.Context) (*Schema, error)

	// Tables returns the list of available table names.
	Tables(ctx context.Context) ([]string, error)

	// Columns returns the columns for the requested tables.
	// The keys in the returned map correspond to the requested table names.
	Columns(ctx context.Context, tables []string) (map[string][]Column, error)

	// ColumnValues returns possible values for the requested columns.
	// Implementations that do not support value enumeration should return
	// an empty map and nil error.
	ColumnValues(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error)
}

// NewSchemaHandlerFromProvider creates a SchemaHandler that routes requests
// to the appropriate TableSchemaProvider method based on the request type.
func NewSchemaHandlerFromProvider(p TableSchemaProvider) SchemaHandler {
	return &providerHandler{provider: p}
}

type providerHandler struct {
	provider TableSchemaProvider
}

func (h *providerHandler) Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
	switch req.Type {
	case RequestTypeTables:
		tables, err := h.provider.Tables(ctx)
		if err != nil {
			return nil, err
		}
		return &SchemaResponse{Tables: tables}, nil
	case RequestTypeColumns:
		cols, err := h.provider.Columns(ctx, req.Tables)
		if err != nil {
			return nil, err
		}
		return &SchemaResponse{Columns: cols}, nil
	case RequestTypeValues:
		vals, err := h.provider.ColumnValues(ctx, req.Columns)
		if err != nil {
			return nil, err
		}
		return &SchemaResponse{ColumnValues: vals}, nil
	case RequestTypeFullSchema:
	default:
		schema, err := h.provider.FullSchema(ctx)
		if err != nil {
			return nil, err
		}
		if schema == nil {
			return &SchemaResponse{}, nil
		}
		return &SchemaResponse{FullSchema: *schema}, nil
	}
	return nil, fmt.Errorf("invalid request type: %s", req.Type)
}
