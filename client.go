package schemas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// FetchSchema sends a [SchemaRequest] to a plugin's schema resource endpoint
// over HTTP and returns the decoded [SchemaResponse].
//
// schemaURL must be the full URL to the schema resource (e.g.
// "https://host/api/ds/uid/resources/schema"). Headers set on
// [SchemaRequest.Headers] are forwarded as HTTP request headers.
func FetchSchema(ctx context.Context, httpClient *http.Client, schemaURL string, req *SchemaRequest) (*SchemaResponse, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("httpClient is required")
	}

	if schemaURL == "" {
		return nil, fmt.Errorf("schemaURL is required")
	}

	parsed, err := url.Parse(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("schemads: invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("schemads: unsupported URL scheme %q", parsed.Scheme)
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("schemads: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("schemads: failed to create HTTP request: %w", err)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("schemads: request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.DefaultLogger.Warn("schemads: failed to close response body: %w", err)
		}
	}()

	if resp.StatusCode == http.StatusNotImplemented {
		return nil, ErrSchemaNotImplemented
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("schemads: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var schema SchemaResponse
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return nil, fmt.Errorf("schemads: failed to decode response: %w", err)
	}
	return &schema, nil
}
