package schemas

import (
	"encoding/json"
	"errors"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

var ErrSchemaNotImplemented = errors.New("schema not implemented")
var ErrTablesNotImplemented = errors.New("tables not implemented")
var ErrColumnsNotImplemented = errors.New("columns not implemented")
var ErrTableParameterValuesNotImplemented = errors.New("table parameter values not implemented")
var ErrColumnValuesNotImplemented = errors.New("column values not implemented")

func sendSchemaError(sender backend.CallResourceResponseSender, status int, message string) error {
	body, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		body = []byte(`{"error":"failed to marshal schema error"}`)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
}
