package schemas

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/stretchr/testify/require"
)

func TestSchemaDatasourcePassthrough(t *testing.T) {
	ctx := context.Background()
	settings := backend.DataSourceInstanceSettings{}
	called := false
	next := backend.CallResourceHandlerFunc(func(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
		called = true
		return sender.Send(&backend.CallResourceResponse{Status: 200, Body: []byte("ok")})
	})

	schemaDs := NewSchemaDatasource(nil, next)
	_, err := schemaDs.NewDatasource(ctx, settings)
	require.NoError(t, err)
	var sent *backend.CallResourceResponse
	sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error {
		sent = r
		return nil
	})
	err = schemaDs.CallResource(ctx, &backend.CallResourceRequest{
		Path: "/other",
	}, sender)
	require.NoError(t, err)
	require.True(t, called)
	require.NotNil(t, sent)
	require.Equal(t, 200, sent.Status)
}

func TestSchemaDatasourceNotImplemented(t *testing.T) {
	ctx := context.Background()
	settings := backend.DataSourceInstanceSettings{}

	schemaDs := NewSchemaDatasource(nil, nil)
	_, err := schemaDs.NewDatasource(ctx, settings)
	require.NoError(t, err)
	var sent *backend.CallResourceResponse
	sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error {
		sent = r
		return nil
	})
	err = schemaDs.CallResource(ctx, &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte("{}"),
	}, sender)
	require.NoError(t, err)
	require.NotNil(t, sent)
	require.Equal(t, 501, sent.Status)
	var body map[string]string
	require.NoError(t, json.Unmarshal(sent.Body, &body))
	require.Equal(t, "schema not implemented", body["error"])
}

func TestSchemaDatasourceSchemaPath(t *testing.T) {
	ctx := context.Background()
	settings := backend.DataSourceInstanceSettings{}
	want := &SchemaResponse{
		Tables: []string{"t1"},
	}
	handler := SchemaHandlerFunc(func(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
		return want, nil
	})

	schemaDs := NewSchemaDatasource(handler, nil)
	_, err := schemaDs.NewDatasource(ctx, settings)
	require.NoError(t, err)
	var sent *backend.CallResourceResponse
	sender := backend.CallResourceResponseSenderFunc(func(r *backend.CallResourceResponse) error {
		sent = r
		return nil
	})
	err = schemaDs.CallResource(ctx, &backend.CallResourceRequest{
		Path: SchemaResourcePath,
		Body: []byte(`{"type":"tables"}`),
	}, sender)
	require.NoError(t, err)
	require.NotNil(t, sent)
	require.Equal(t, 200, sent.Status)
	var resp SchemaResponse
	require.NoError(t, json.Unmarshal(sent.Body, &resp))
	require.Equal(t, []string{"t1"}, resp.Tables)
}

func TestValidateRequest(t *testing.T) {
	t.Run("empty type allowed", func(t *testing.T) {
		require.NoError(t, ValidateRequest(&SchemaRequest{Type: ""}))
	})
	t.Run("columns without tables", func(t *testing.T) {
		err := ValidateRequest(&SchemaRequest{Type: "columns", Tables: nil})
		require.Error(t, err)
	})
	t.Run("values without columns", func(t *testing.T) {
		err := ValidateRequest(&SchemaRequest{Type: "values", Columns: nil})
		require.Error(t, err)
	})
	t.Run("invalid type", func(t *testing.T) {
		err := ValidateRequest(&SchemaRequest{Type: "invalid"})
		require.Error(t, err)
	})
}
