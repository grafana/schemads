package schemas

import (
	"encoding/json"
	"errors"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

var (
	ErrSchemaNotImplemented = errors.New("schema not implemented")
)

func sendSchemaError(sender backend.CallResourceResponseSender, status int, message string) error {
	body, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		body = []byte(`{"error":"internal error"}`)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
}
