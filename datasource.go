package schemas

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// SchemaDatasource is a [backend.CallResourceHandler] that intercepts requests
// under [BaseResourcePath] and delegates them to the configured handlers. All
// other resource paths are forwarded to CallResourceHandler (if set) or return 404.
type SchemaDatasource struct {
	// The Schema handlers serve the different schema endpoints. If nil, those requests
	// return 501 Not Implemented.
	SchemaHandler               SchemaHandler
	TablesHandler               TablesHandler
	ColumnsHandler              ColumnsHandler
	TableParameterValuesHandler TableParameterValuesHandler
	ColumnValuesHandler         ColumnValuesHandler

	// The CallResourceHandler is used to forward requests that are not handled by the schema handlers.
	CallResourceHandler backend.CallResourceHandler
}

// NewSchemaDatasource creates a [SchemaDatasource]. Pass nil for
// schemaHandler to return 501 for schema requests. Pass nil for next to
// return 404 for non-schema paths.
func NewSchemaDatasource(schemaHandler SchemaHandler, tablesHandler TablesHandler, columnsHandler ColumnsHandler, tableParameterValuesHandler TableParameterValuesHandler, columnValuesHandler ColumnValuesHandler, next backend.CallResourceHandler) *SchemaDatasource {
	return &SchemaDatasource{
		SchemaHandler:               schemaHandler,
		TablesHandler:               tablesHandler,
		ColumnsHandler:              columnsHandler,
		TableParameterValuesHandler: tableParameterValuesHandler,
		ColumnValuesHandler:         columnValuesHandler,
		CallResourceHandler:         next,
	}
}

// CallResource implements [backend.CallResourceHandler]. Requests whose path
// starts with [BaseResourcePath] are handled internally; everything else is
// forwarded to CallResourceHandler.
func (ds *SchemaDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req != nil && strings.HasPrefix(req.Path, BaseResourcePath) {

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
	path := strings.TrimLeft(strings.TrimPrefix(req.Path, BaseResourcePath), "/")
	headers := extractHeaders(req)

	var data []byte
	switch path {
	case RequestTypeFullSchema:
		if ds.SchemaHandler == nil {
			return sendSchemaError(sender, http.StatusNotImplemented, ErrSchemaNotImplemented.Error())
		}
		response, err := ds.SchemaHandler.Schema(ctx, &SchemaRequest{
			Headers: headers,
		})
		if err != nil {
			return err
		}
		if err := ValidateSchema(response.FullSchema); err != nil {
			return sendSchemaError(sender, http.StatusInternalServerError, "schema validation failed: "+err.Error())
		}
		data, err = json.Marshal(response)
		if err != nil {
			return err
		}
	case RequestTypeTables:
		if ds.TablesHandler == nil {
			return sendSchemaError(sender, http.StatusNotImplemented, ErrTablesNotImplemented.Error())
		}
		response, err := ds.TablesHandler.Tables(ctx, &TablesRequest{
			Headers: headers,
		})
		if err != nil {
			return err
		}
		data, err = json.Marshal(response)
		if err != nil {
			return err
		}
	case RequestTypeColumns:
		if ds.ColumnsHandler == nil {
			return sendSchemaError(sender, http.StatusNotImplemented, ErrColumnsNotImplemented.Error())
		}
		request := &ColumnsRequest{}
		if err := json.Unmarshal(req.Body, request); err != nil {
			return err
		}
		request.Headers = headers
		response, err := ds.ColumnsHandler.Columns(ctx, request)
		if err != nil {
			return err
		}
		data, err = json.Marshal(response)
		if err != nil {
			return err
		}
	case RequestTypeTableParameterValues:
		if ds.TableParameterValuesHandler == nil {
			return sendSchemaError(sender, http.StatusNotImplemented, ErrTableParameterValuesNotImplemented.Error())
		}
		request := &TableParameterValuesRequest{}
		if err := json.Unmarshal(req.Body, request); err != nil {
			return err
		}
		request.Headers = headers
		response, err := ds.TableParameterValuesHandler.TableParameterValues(ctx, request)
		if err != nil {
			return err
		}
		data, err = json.Marshal(response)
		if err != nil {
			return err
		}
	case RequestTypeColumnValues:
		if ds.ColumnValuesHandler == nil {
			return sendSchemaError(sender, http.StatusNotImplemented, ErrColumnValuesNotImplemented.Error())
		}
		request := &ColumnValuesRequest{}
		if err := json.Unmarshal(req.Body, request); err != nil {
			return err
		}
		request.Headers = headers
		response, err := ds.ColumnValuesHandler.ColumnValues(ctx, request)
		if err != nil {
			return err
		}
		data, err = json.Marshal(response)
		if err != nil {
			return err
		}
	default:
		return sender.Send(&backend.CallResourceResponse{
			Status: 404,
			Body:   []byte(ErrSchemaNotImplemented.Error()),
		})
	}

	return sender.Send(&backend.CallResourceResponse{
		Status: http.StatusOK,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: data,
	})
}
