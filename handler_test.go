package schemas

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockProvider implements TableSchemaProvider for testing.
type mockProvider struct {
	fullSchema     func(ctx context.Context) (*Schema, error)
	tables         func(ctx context.Context) ([]string, map[string][]SubTable, error)
	columns        func(ctx context.Context, tables []string) (map[string][]Column, error)
	columnValues   func(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error)
	subTableValues func(ctx context.Context, subTables []SubTableValuesRequest) (map[string][]string, error)
}

func (m *mockProvider) FullSchema(ctx context.Context) (*Schema, error) {
	return m.fullSchema(ctx)
}

func (m *mockProvider) Tables(ctx context.Context) ([]string, map[string][]SubTable, error) {
	return m.tables(ctx)
}

func (m *mockProvider) Columns(ctx context.Context, tables []string) (map[string][]Column, error) {
	return m.columns(ctx, tables)
}

func (m *mockProvider) ColumnValues(ctx context.Context, columns []ColumnValuesRequest) (map[string][]string, error) {
	return m.columnValues(ctx, columns)
}

func (m *mockProvider) SubTableValues(ctx context.Context, subTables []SubTableValuesRequest) (map[string][]string, error) {
	return m.subTableValues(ctx, subTables)
}

func TestNewSchemaHandlerFromProvider(t *testing.T) {
	t.Run("routes tables request", func(t *testing.T) {
		p := &mockProvider{
			tables: func(_ context.Context) ([]string, map[string][]SubTable, error) {
				return []string{"a", "b"}, nil, nil
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: RequestTypeTables})
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, resp.Tables)
		require.Nil(t, resp.SubTables)
	})

	t.Run("routes tables request with sub-tables", func(t *testing.T) {
		wantSubTables := map[string][]SubTable{
			"issues": {
				{Name: "organization", Root: true, Required: true},
				{Name: "repository", DependsOn: []string{"organization"}, Required: true},
			},
		}
		p := &mockProvider{
			tables: func(_ context.Context) ([]string, map[string][]SubTable, error) {
				return []string{"issues", "users"}, wantSubTables, nil
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: RequestTypeTables})
		require.NoError(t, err)
		require.Equal(t, []string{"issues", "users"}, resp.Tables)
		require.Equal(t, wantSubTables, resp.SubTables)
		require.Len(t, resp.SubTables["issues"], 2)
		require.True(t, resp.SubTables["issues"][0].Root)
		require.True(t, resp.SubTables["issues"][0].Required)
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

	t.Run("unrecognized type errors", func(t *testing.T) {
		want := &Schema{
			Tables: []Table{{Name: "orders", Columns: []Column{{Name: "id", Type: ColumnTypeInt64}}}},
		}
		p := &mockProvider{
			fullSchema: func(_ context.Context) (*Schema, error) { return want, nil },
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{Type: "unknown"})
		require.ErrorContains(t, err, "invalid request type: unknown")
	})

	t.Run("propagates error from Tables", func(t *testing.T) {
		p := &mockProvider{
			tables: func(_ context.Context) ([]string, map[string][]SubTable, error) {
				return nil, nil, fmt.Errorf("tables boom")
			},
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

	t.Run("routes fullSchema with sub-tables", func(t *testing.T) {
		want := &Schema{
			Tables: []Table{{
				Name: "issues",
				SubTables: []SubTable{
					{Name: "organization", Root: true, Required: true},
					{Name: "repository", DependsOn: []string{"organization"}, Required: true},
				},
				Columns: []Column{{Name: "title", Type: ColumnTypeString}},
			}},
			SubTableValues: map[string]map[string][]string{
				"issues": {"organization": {"grafana", "kubernetes"}},
			},
		}
		p := &mockProvider{
			fullSchema: func(_ context.Context) (*Schema, error) { return want, nil },
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{Type: RequestTypeFullSchema})
		require.NoError(t, err)
		require.Len(t, resp.FullSchema.Tables, 1)
		require.Len(t, resp.FullSchema.Tables[0].SubTables, 2)
		require.True(t, resp.FullSchema.Tables[0].SubTables[0].Root)
		require.True(t, resp.FullSchema.Tables[0].SubTables[0].Required)
		require.Equal(t, []string{"organization"}, resp.FullSchema.Tables[0].SubTables[1].DependsOn)
		require.Equal(t, []string{"grafana", "kubernetes"},
			resp.FullSchema.SubTableValues["issues"]["organization"])
	})

	t.Run("routes subTableValues request", func(t *testing.T) {
		reqs := []SubTableValuesRequest{
			{Table: "issues", SubTable: "organization"},
		}
		p := &mockProvider{
			subTableValues: func(_ context.Context, st []SubTableValuesRequest) (map[string][]string, error) {
				require.Equal(t, reqs, st)
				return map[string][]string{
					"issues_organization": {"grafana", "kubernetes"},
				}, nil
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		resp, err := h.Schema(context.Background(), &SchemaRequest{
			Type:      RequestTypeSubTableValues,
			SubTables: reqs,
		})
		require.NoError(t, err)
		require.Equal(t, []string{"grafana", "kubernetes"},
			resp.SubTableValues["issues_organization"])
	})

	t.Run("propagates error from SubTableValues", func(t *testing.T) {
		p := &mockProvider{
			subTableValues: func(_ context.Context, _ []SubTableValuesRequest) (map[string][]string, error) {
				return nil, fmt.Errorf("sub-table values boom")
			},
		}

		h := NewSchemaHandlerFromProvider(p)
		_, err := h.Schema(context.Background(), &SchemaRequest{
			Type:      RequestTypeSubTableValues,
			SubTables: []SubTableValuesRequest{{Table: "issues", SubTable: "organization"}},
		})
		require.EqualError(t, err, "sub-table values boom")
	})
}
