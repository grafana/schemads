package schemas

import (
	"encoding/json"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	SchemaResourcePath = "schema"
)

type schemaRequestBody struct {
	Type    string                      `json:"type"`
	Tables  []string                    `json:"tables"`
	Columns []ColumnsInformationRequest `json:"columns"`
}

func parseSchemaRequest(req *backend.CallResourceRequest) (*SchemaRequest, error) {
	body := schemaRequestBody{}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return nil, err
		}
	}
	headers := make(map[string]string)
	for k, v := range req.Headers {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return &SchemaRequest{
		Headers: headers,
		Type:    body.Type,
		Tables:  body.Tables,
		Columns: body.Columns,
	}, nil
}
