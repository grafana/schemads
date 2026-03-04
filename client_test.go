package schemas

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchSchema_success(t *testing.T) {
	want := &SchemaResponse{
		Tables: []string{"issues", "users"},
		SubTables: map[string][]SubTable{
			"issues": {
				{Name: "organization", Root: true, Required: true},
				{Name: "repository", DependsOn: []string{"organization"}, Required: true},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	got, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{
		Type: RequestTypeTables,
	})
	require.NoError(t, err)
	require.Equal(t, want.Tables, got.Tables)
	require.Len(t, got.SubTables["issues"], 2)
	require.True(t, got.SubTables["issues"][0].Root)
	require.True(t, got.SubTables["issues"][0].Required)
	require.Equal(t, []string{"organization"}, got.SubTables["issues"][1].DependsOn)
}

func TestFetchSchema_501_returns_ErrSchemaNotImplemented(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.ErrorIs(t, err, ErrSchemaNotImplemented)
}

func TestFetchSchema_non200_returns_error_with_body(t *testing.T) {
	const bodyMsg = "internal failure details"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(bodyMsg))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	require.Contains(t, err.Error(), bodyMsg)
}

func TestFetchSchema_non200_body_truncated_at_1024(t *testing.T) {
	longBody := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.Error(t, err)
	require.LessOrEqual(t, len(err.Error()), len(longBody), "error should not contain the full 2048-byte body")
	require.Contains(t, err.Error(), strings.Repeat("x", 1024))
}

func TestFetchSchema_invalid_url_scheme(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "ftp scheme", url: "ftp://example.com/schema"},
		{name: "empty scheme", url: "://example.com/schema"},
		{name: "no scheme", url: "example.com/schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FetchSchema(context.Background(), http.DefaultClient, tc.url, &SchemaRequest{})
			require.Error(t, err)
			require.Contains(t, err.Error(), "schemads:")
		})
	}
}

func TestFetchSchema_request_body_contains_full_schema_request(t *testing.T) {
	sent := &SchemaRequest{
		Type:   RequestTypeValues,
		Tables: []string{"events"},
		Columns: []ColumnValuesRequest{
			{Table: "events", Columns: []string{"status"}, Parameters: map[string]string{"org": "foo"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var received SchemaRequest
		require.NoError(t, json.Unmarshal(body, &received))
		require.Equal(t, sent.Type, received.Type)
		require.Equal(t, sent.Tables, received.Tables)
		require.Len(t, received.Columns, 1)
		require.Equal(t, "events", received.Columns[0].Table)
		require.Equal(t, "status", received.Columns[0].Columns[0])
		require.Equal(t, "foo", received.Columns[0].Parameters["org"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&SchemaResponse{}))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, sent)
	require.NoError(t, err)
}

func TestFetchSchema_sets_headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.Equal(t, http.MethodPost, r.Method)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&SchemaResponse{}))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.NoError(t, err)
}

func TestFetchSchema_forwards_request_headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok123", r.Header.Get("Authorization"))
		require.Equal(t, "custom-val", r.Header.Get("X-Custom"))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&SchemaResponse{}))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{
		Headers: map[string]string{
			"Authorization": "Bearer tok123",
			"X-Custom":      "custom-val",
		},
	})
	require.NoError(t, err)
}

func TestFetchSchema_cancelled_context(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&SchemaResponse{}))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchSchema(ctx, srv.Client(), srv.URL, &SchemaRequest{})
	require.Error(t, err)
}

func TestFetchSchema_nil_httpClient_returns_error(t *testing.T) {
	_, err := FetchSchema(context.Background(), nil, "http://example.com", &SchemaRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "httpClient is required")
}

func TestFetchSchema_empty_url_returns_error(t *testing.T) {
	_, err := FetchSchema(context.Background(), http.DefaultClient, "", &SchemaRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "schemaURL is required")
}

func TestFetchSchema_decodes_full_response(t *testing.T) {
	want := &SchemaResponse{
		FullSchema: &Schema{
			Tables: []Table{
				{
					Name: "issues",
					SubTables: []SubTable{
						{Name: "organization", Root: true, Required: true},
						{Name: "repository", DependsOn: []string{"organization"}, Required: true},
					},
					Columns: []Column{
						{Name: "ts", Type: ColumnTypeTimestamp},
						{Name: "value", Type: ColumnTypeFloat64},
					},
				},
			},
			Functions: []string{"avg", "sum"},
			SubTableValues: map[string]map[string][]string{
				"issues": {
					"organization": {"grafana", "kubernetes"},
				},
			},
		},
		Columns: map[string][]Column{
			"issues": {{Name: "ts", Type: ColumnTypeTimestamp}},
		},
		ColumnValues: map[string][]string{
			"region": {"us-east-1", "eu-west-1"},
		},
		Errors: map[string]string{
			"broken_table": "timeout",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	got, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.NoError(t, err)
	require.Equal(t, want.FullSchema.Tables[0].Name, got.FullSchema.Tables[0].Name)
	require.Equal(t, want.FullSchema.Functions, got.FullSchema.Functions)
	require.Equal(t, want.Columns, got.Columns)
	require.Equal(t, want.ColumnValues, got.ColumnValues)
	require.Equal(t, want.Errors, got.Errors)

	tbl := got.FullSchema.Tables[0]
	require.Len(t, tbl.SubTables, 2)
	require.True(t, tbl.SubTables[0].Root)
	require.True(t, tbl.SubTables[0].Required)
	require.Equal(t, "organization", tbl.SubTables[0].Name)
	require.Equal(t, []string{"organization"}, tbl.SubTables[1].DependsOn)
	require.True(t, tbl.SubTables[1].Required)
	require.Equal(t, []string{"grafana", "kubernetes"},
		got.FullSchema.SubTableValues["issues"]["organization"])
}

func TestFetchSchema_subtable_values_round_trip(t *testing.T) {
	want := &SchemaResponse{
		SubTableValues: map[string][]string{
			"issues_organization": {"grafana", "kubernetes"},
			"issues_repository":   {"grafana", "loki", "mimir"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var received SchemaRequest
		require.NoError(t, json.Unmarshal(body, &received))
		require.Equal(t, RequestTypeSubTableValues, received.Type)
		require.Len(t, received.SubTables, 1)
		require.Equal(t, "issues", received.SubTables[0].Table)
		require.Equal(t, "repository", received.SubTables[0].SubTable)
		require.Equal(t, "grafana", received.SubTables[0].DependencyValues["organization"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	got, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{
		Type: RequestTypeSubTableValues,
		SubTables: []SubTableValuesRequest{
			{
				Table:            "issues",
				SubTable:         "repository",
				DependencyValues: map[string]string{"organization": "grafana"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"grafana", "kubernetes"},
		got.SubTableValues["issues_organization"])
	require.Equal(t, []string{"grafana", "loki", "mimir"},
		got.SubTableValues["issues_repository"])
}

func TestFetchSchema_invalid_json_response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	_, err := FetchSchema(context.Background(), srv.Client(), srv.URL, &SchemaRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to decode response")
}
