package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/require"
)

func newTestSender() (backend.CallResourceResponseSender, func() *backend.CallResourceResponse) {
	var sent *backend.CallResourceResponse
	sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error {
		sent = r
		return nil
	})
	return sender, func() *backend.CallResourceResponse { return sent }
}

func decodeErrorBody(t *testing.T, body []byte) string {
	t.Helper()
	var m map[string]string
	require.NoError(t, json.Unmarshal(body, &m))
	return m["error"]
}

func TestCallResource_passthrough(t *testing.T) {
	called := false
	next := backend.CallResourceHandlerFunc(func(_ context.Context, _ *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
		called = true
		return sender.Send(&backend.CallResourceResponse{Status: 200, Body: []byte("ok")})
	})

	ds := NewSchemaDatasource(nil, next)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "/other"}, sender)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, 200, get().Status)
}

func TestCallResource_nil_next_returns_404(t *testing.T) {
	ds := NewSchemaDatasource(nil, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "/anything"}, sender)
	require.NoError(t, err)
	require.Equal(t, 404, get().Status)
}

func TestCallResource_nil_request_returns_404(t *testing.T) {
	ds := NewSchemaDatasource(nil, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), nil, sender)
	require.NoError(t, err)
	require.Equal(t, 404, get().Status)
}

func TestCallResource_schema_not_implemented(t *testing.T) {
	ds := NewSchemaDatasource(nil, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte("{}"),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 501, get().Status)
	require.Equal(t, ErrSchemaNotImplemented.Error(), decodeErrorBody(t, get().Body))
}

func TestCallResource_schema_full(t *testing.T) {
	want := &SchemaResponse{
		FullSchema: Schema{
			Tables: []Table{{Name: "users", Columns: []Column{
				{Name: "id", Type: ColumnTypeNumber},
				{Name: "name", Type: ColumnTypeString},
			}}},
		},
	}
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return want, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)

	var resp SchemaResponse
	require.NoError(t, json.Unmarshal(get().Body, &resp))
	require.Len(t, resp.FullSchema.Tables, 1)
	require.Equal(t, "users", resp.FullSchema.Tables[0].Name)
	require.Len(t, resp.FullSchema.Tables[0].Columns, 2)
}

func TestCallResource_schema_tables(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{Tables: []string{"t1", "t2"}}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"tables"}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)

	var resp SchemaResponse
	require.NoError(t, json.Unmarshal(get().Body, &resp))
	require.Equal(t, []string{"t1", "t2"}, resp.Tables)
}

func TestCallResource_schema_columns(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, req *SchemaRequest) (*SchemaResponse, error) {
		require.Equal(t, []string{"users"}, req.Tables)
		return &SchemaResponse{
			Columns: map[string][]Column{
				"users": {{Name: "id", Type: ColumnTypeNumber}},
			},
		}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"columns","tables":["users"]}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)

	var resp SchemaResponse
	require.NoError(t, json.Unmarshal(get().Body, &resp))
	require.Contains(t, resp.Columns, "users")
	require.Len(t, resp.Columns["users"], 1)
}

func TestCallResource_schema_empty_body(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{Tables: []string{"default"}}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)
}

func TestCallResource_schema_handler_error(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return nil, fmt.Errorf("database connection failed")
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 500, get().Status)
	require.Equal(t, "database connection failed", decodeErrorBody(t, get().Body))
}

func TestCallResource_schema_handler_nil_response(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return nil, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)
	require.Equal(t, "null", string(get().Body))
}

func TestCallResource_schema_malformed_json(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{not json`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 400, get().Status)
	require.Contains(t, decodeErrorBody(t, get().Body), "invalid request")
}

func TestCallResource_schema_invalid_type(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"ymmv"}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 400, get().Status)
	require.Contains(t, decodeErrorBody(t, get().Body), "invalid table information request type")
}

func TestCallResource_schema_columns_missing_tables(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"columns"}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 400, get().Status)
	require.Contains(t, decodeErrorBody(t, get().Body), "tables must be specified")
}

func TestCallResource_schema_values_missing_columns(t *testing.T) {
	handler := SchemaHandlerFunc(func(_ context.Context, _ *SchemaRequest) (*SchemaResponse, error) {
		return &SchemaResponse{}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"values"}`),
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 400, get().Status)
	require.Contains(t, decodeErrorBody(t, get().Body), "columns must be specified")
}

func TestCallResource_schema_propagates_headers(t *testing.T) {
	var received map[string]string
	handler := SchemaHandlerFunc(func(_ context.Context, req *SchemaRequest) (*SchemaResponse, error) {
		received = req.Headers
		return &SchemaResponse{}, nil
	})

	ds := NewSchemaDatasource(handler, nil)
	sender, get := newTestSender()
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{}`),
		Headers: map[string][]string{
			"Authorization": {"Bearer tok"},
			"X-Custom":      {"val1", "val2"},
		},
	}, sender)
	require.NoError(t, err)
	require.Equal(t, 200, get().Status)
	require.Equal(t, "Bearer tok", received["Authorization"])
	require.Equal(t, "val1", received["X-Custom"])
}

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *SchemaRequest
		wantErr string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: "must not be nil",
		},
		{
			name: "empty type allowed",
			req:  &SchemaRequest{Type: ""},
		},
		{
			name: "tables type allowed",
			req:  &SchemaRequest{Type: "tables"},
		},
		{
			name: "columns with tables",
			req:  &SchemaRequest{Type: "columns", Tables: []string{"t1"}},
		},
		{
			name: "values with columns",
			req: &SchemaRequest{Type: "values", Columns: []ColumnsInformationRequest{
				{Table: "t1"},
			}},
		},
		{
			name:    "invalid type",
			req:     &SchemaRequest{Type: "invalid"},
			wantErr: "invalid table information request type",
		},
		{
			name:    "columns without tables",
			req:     &SchemaRequest{Type: "columns"},
			wantErr: "tables must be specified",
		},
		{
			name:    "values without columns",
			req:     &SchemaRequest{Type: "values"},
			wantErr: "columns must be specified",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequest(tc.req)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestParseSchemaRequest(t *testing.T) {
	t.Run("full body", func(t *testing.T) {
		req, err := parseSchemaRequest(&backend.CallResourceRequest{
			Body: []byte(`{"type":"columns","tables":["t1","t2"],"columns":[{"table":"t1","parameters":{"k":"v"}}]}`),
			Headers: map[string][]string{
				"X-Test": {"value"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "columns", req.Type)
		require.Equal(t, []string{"t1", "t2"}, req.Tables)
		require.Len(t, req.Columns, 1)
		require.Equal(t, "t1", req.Columns[0].Table)
		require.Equal(t, "v", req.Columns[0].Parameters["k"])
		require.Equal(t, "value", req.Headers["X-Test"])
	})

	t.Run("empty body", func(t *testing.T) {
		req, err := parseSchemaRequest(&backend.CallResourceRequest{})
		require.NoError(t, err)
		require.Equal(t, "", req.Type)
		require.Empty(t, req.Tables)
		require.Empty(t, req.Columns)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := parseSchemaRequest(&backend.CallResourceRequest{
			Body: []byte(`{broken`),
		})
		require.Error(t, err)
	})

	t.Run("empty headers", func(t *testing.T) {
		req, err := parseSchemaRequest(&backend.CallResourceRequest{
			Body: []byte(`{}`),
			Headers: map[string][]string{
				"Empty": {},
			},
		})
		require.NoError(t, err)
		_, hasEmpty := req.Headers["Empty"]
		require.False(t, hasEmpty)
	})
}
