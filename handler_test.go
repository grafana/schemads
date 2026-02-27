package schemas

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockProvider implements TableSchemaProvider for testing.
type mockProvider struct {
	fullSchema   func(ctx context.Context) (*Schema, error)
	tables       func(ctx context.Context) ([]string, error)
	columns      func(ctx context.Context, tables []string) (map[string][]Column, error)
	columnValues func(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error)
}

func (m *mockProvider) FullSchema(ctx context.Context) (*Schema, error) {
	return m.fullSchema(ctx)
}

func (m *mockProvider) Tables(ctx context.Context) ([]string, error) {
	return m.tables(ctx)
}

func (m *mockProvider) Columns(ctx context.Context, tables []string) (map[string][]Column, error) {
	return m.columns(ctx, tables)
}

func (m *mockProvider) ColumnValues(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error) {
	return m.columnValues(ctx, columns)
}

func TestNewSchemaHandlerFromProvider(t *testing.T) {
	t.Run("routes tables request", func(t *testing.T) {
		p := &mockProvider{
			tables: func(_ context.Context) ([]string, error) { return []string{"a", "b"}, nil },
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: RequestTypeTables})
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, resp.Tables)
	})

	t.Run("routes columns request and forwards table names", func(t *testing.T) {
		p := &mockProvider{
			columns: func(_ context.Context, tables []string) (map[string][]Column, error) {
				require.Equal(t, []string{"users"}, tables)
				return map[string][]Column{
					"users": {{Name: "email", Type: ColumnTypeString}},
				}, nil
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{
			Type:   RequestTypeColumns,
			Tables: []string{"users"},
		})
		require.NoError(t, err)
		require.Len(t, resp.Columns["users"], 1)
		require.Equal(t, "email", resp.Columns["users"][0].Name)
	})

	t.Run("routes values request and forwards column requests", func(t *testing.T) {
		cols := []ColumnValuesRequest{{Table: "t1", Parameters: map[string]string{"col": "status"}}}
		p := &mockProvider{
			columnValues: func(_ context.Context, c []ColumnValuesRequest) (map[string][]string, error) {
				require.Equal(t, cols, c)
				return map[string][]string{"status": {"active", "inactive"}}, nil
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{
			Type:    RequestTypeValues,
			Columns: cols,
		})
		require.NoError(t, err)
		require.Equal(t, []string{"active", "inactive"}, resp.ColumnValues["status"])
	})

	t.Run("unrecognized type routes to FullSchema via default", func(t *testing.T) {
		want := &Schema{
			Tables: []Table{{Name: "orders", Columns: []Column{{Name: "id", Type: ColumnTypeInt64}}}},
		}
		p := &mockProvider{
			fullSchema: func(_ context.Context) (*Schema, error) { return want, nil },
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: "unknown"})
		require.NoError(t, err)
		require.Equal(t, *want, resp.FullSchema)
	})

	t.Run("propagates error from Tables", func(t *testing.T) {
		p := &mockProvider{
			tables: func(_ context.Context) ([]string, error) { return nil, fmt.Errorf("tables boom") },
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{Type: RequestTypeTables})
		require.EqualError(t, err, "tables boom")
	})

	t.Run("propagates error from Columns", func(t *testing.T) {
		p := &mockProvider{
			columns: func(_ context.Context, _ []string) (map[string][]Column, error) {
				return nil, fmt.Errorf("columns boom")
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{
			Type:   RequestTypeColumns,
			Tables: []string{"t1"},
		})
		require.EqualError(t, err, "columns boom")
	})

	t.Run("propagates error from ColumnValues", func(t *testing.T) {
		p := &mockProvider{
			columnValues: func(_ context.Context, _ []ColumnValuesRequest) (map[string][]string, error) {
				return nil, fmt.Errorf("values boom")
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{
			Type:    RequestTypeValues,
			Columns: []ColumnValuesRequest{{Table: "t1"}},
		})
		require.EqualError(t, err, "values boom")
	})

	t.Run("propagates error from FullSchema via default", func(t *testing.T) {
		p := &mockProvider{
			fullSchema: func(_ context.Context) (*Schema, error) { return nil, fmt.Errorf("full schema boom") },
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{Type: "unhandled"})
		require.EqualError(t, err, "full schema boom")
	})

	t.Run("nil schema from FullSchema via default returns empty response", func(t *testing.T) {
		p := &mockProvider{
			fullSchema: func(_ context.Context) (*Schema, error) { return nil, nil },
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: "unhandled"})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Empty(t, resp.FullSchema.Tables)
	})
}
