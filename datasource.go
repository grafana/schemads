package schemas

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
)

// SchemaDatasource wraps a Grafana data source to add support for schema requests
// (full schema, tables, columns, column values) via resource calls.
type SchemaDatasource struct {
	// SchemaHandler provides tabular information. If nil, requests to
	// SchemaResourcePath return 501 Not Implemented.
	SchemaHandler SchemaHandler

	// CallResourceHandler is the next handler for resource calls. Set by
	// NewSchemaDatasource(handler, next). When a request is not for the schema
	// path, it is delegated here. Can be nil.
	CallResourceHandler backend.CallResourceHandler
}

func NewSchemaDatasource(schemaHandler SchemaHandler, next backend.CallResourceHandler) *SchemaDatasource {
	return &SchemaDatasource{
		SchemaHandler:       schemaHandler,
		CallResourceHandler: next,
	}
}

func (ds *SchemaDatasource) NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	return ds, nil
}

func (ds *SchemaDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req != nil && req.Path == SchemaResourcePath {
		return ds.handleSchemaResource(ctx, req, sender)
	}
	if ds.CallResourceHandler != nil {
		return ds.CallResourceHandler.CallResource(ctx, req, sender)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: 404,
		Body:   []byte("not found"),
	})
}

func (ds *SchemaDatasource) handleSchemaResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if ds.SchemaHandler == nil {
		return sendSchemaError(sender, http.StatusNotImplemented, "schema not implemented")
	}
	tableReq, err := parseSchemaRequest(req)
	if err != nil {
		return sendSchemaError(sender, http.StatusBadRequest, "invalid request: "+err.Error())
	}
	if err := ValidateRequest(tableReq); err != nil {
		return sendSchemaError(sender, http.StatusBadRequest, err.Error())
	}

	resp, err := ds.SchemaHandler.Schema(ctx, tableReq)
	if err != nil {
		return sendSchemaError(sender, http.StatusInternalServerError, err.Error())
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return sendSchemaError(sender, http.StatusInternalServerError, err.Error())
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: data,
	})
}
