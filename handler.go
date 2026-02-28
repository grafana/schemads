package schemas

import (
	"context"
	"fmt"
)

// TableSchemaProvider is a higher-level interface that breaks schema handling
// into one method per request type. Use [NewSchemaHandlerFromProvider] to
// convert it into a [SchemaHandler] with automatic routing.
type TableSchemaProvider interface {
	// FullSchema returns the complete schema. Column values are not included;
	// callers should request those separately with "values".
	// Schema.SubTableValues should only contain values for root sub-tables;
	// non-root values must be fetched via SubTableValues with dependency context.
	FullSchema(ctx context.Context) (*Schema, error)

	// Tables returns table names and, for each table that defines sub-tables,
	// its sub-table definitions. Implementations without sub-tables may
	// return a nil map.
	Tables(ctx context.Context) ([]string, map[string][]SubTable, error)

	// Columns returns columns for the requested tables. Keys in the returned
	// map correspond to the table names in the input slice.
	Columns(ctx context.Context, tables []string) (map[string][]Column, error)

	// ColumnValues returns possible values for the requested columns.
	// Return an empty map and nil error if value enumeration is not supported.
	ColumnValues(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error)

	// SubTableValues returns possible values for the requested sub-tables.
	// Return an empty map and nil error if sub-table value enumeration is
	// not supported.
	SubTableValues(ctx context.Context, subTables []SubTableValuesRequest) (map[string][]string, error)
}

// NewSchemaHandlerFromProvider wraps a [TableSchemaProvider] in a
// [SchemaHandler] that routes each [SchemaRequest] to the matching method.
func NewSchemaHandlerFromProvider(p TableSchemaProvider) SchemaHandler {
	return &providerHandler{provider: p}
}

type providerHandler struct {
	provider TableSchemaProvider
}

func (h *providerHandler) Schema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
	switch req.Type {
	case RequestTypeTables:
		tables, subTables, err := h.provider.Tables(ctx)
		if err != nil {
			return nil, err
		}
		return &SchemaResponse{Tables: tables, SubTables: subTables}, nil
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
		schema, err := h.provider.FullSchema(ctx)
		if err != nil {
			return nil, err
		}
		if schema == nil {
			return &SchemaResponse{}, nil
		}
		return &SchemaResponse{FullSchema: *schema}, nil
	case RequestTypeSubTableValues:
		p, err := h.provider.SubTableValues(ctx, req.SubTables)
		if err != nil {
			return nil, err
		}
		return &SchemaResponse{SubTableValues: p}, nil
	}
	return nil, fmt.Errorf("invalid request type: %s", req.Type)
}
