package schemas

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// SchemaDatasource is a [backend.CallResourceHandler] that intercepts requests
// to [SchemaResourcePath] and delegates them to a [SchemaHandler]. All other
// resource paths are forwarded to CallResourceHandler (if set) or return 404.
type SchemaDatasource struct {
	// SchemaHandler provides tabular information. If nil, requests to
	// SchemaResourcePath return 501 Not Implemented.
	SchemaHandler       SchemaHandler
	CallResourceHandler backend.CallResourceHandler
}

// NewSchemaDatasource creates a [SchemaDatasource]. Pass nil for
// schemaHandler to return 501 for schema requests. Pass nil for next to
// return 404 for non-schema paths.
func NewSchemaDatasource(schemaHandler SchemaHandler, next backend.CallResourceHandler) *SchemaDatasource {
	return &SchemaDatasource{
		SchemaHandler:       schemaHandler,
		CallResourceHandler: next,
	}
}

// CallResource implements [backend.CallResourceHandler]. Requests whose path
// equals [SchemaResourcePath] are handled internally; everything else is
// forwarded to CallResourceHandler.
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
		return sendSchemaError(sender, http.StatusNotImplemented, ErrSchemaNotImplemented.Error())
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
	if tableReq.Type == RequestTypeFullSchema && resp != nil {
		if err := ValidateSchema(resp.FullSchema); err != nil {
			return sendSchemaError(sender, http.StatusInternalServerError, "schema validation failed: "+err.Error())
		}
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
