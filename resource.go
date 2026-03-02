package schemas

import (
	"encoding/json"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	SchemaResourcePath = "schema"
)

func parseSchemaRequest(req *backend.CallResourceRequest) (*SchemaRequest, error) {
	schemaReq := &SchemaRequest{}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, schemaReq); err != nil {
			return nil, err
		}
	}
	headers := make(map[string]string)
	for k, v := range req.Headers {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	schemaReq.Headers = headers
	return schemaReq, nil
}
