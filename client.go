package schemas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(httpClient *http.Client, baseURL string) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("schemads: httpClient is required")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("schemads: baseURL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("schemads: invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("schemads: unsupported URL scheme %q", parsed.Scheme)
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(parsed.String(), "/"),
	}, nil
}

func (c *Client) FetchSchema(ctx context.Context, req *SchemaRequest) (*SchemaResponse, error) {
	var resp SchemaResponse
	if err := c.do(ctx, RequestTypeFullSchema, req.Headers, req, &resp, ErrSchemaNotImplemented); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) FetchTables(ctx context.Context, req *TablesRequest) (*TablesResponse, error) {
	var resp TablesResponse
	if err := c.do(ctx, RequestTypeTables, req.Headers, req, &resp, ErrTablesNotImplemented); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) FetchColumns(ctx context.Context, req *ColumnsRequest) (*ColumnsResponse, error) {
	var resp ColumnsResponse
	if err := c.do(ctx, RequestTypeColumns, req.Headers, req, &resp, ErrColumnsNotImplemented); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) FetchTableParameterValues(ctx context.Context, req *TableParameterValuesRequest) (*TableParametersValuesResponse, error) {
	var resp TableParametersValuesResponse
	if err := c.do(ctx, RequestTypeTableParameterValues, req.Headers, req, &resp, ErrTableParameterValuesNotImplemented); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) FetchColumnValues(ctx context.Context, req *ColumnValuesRequest) (*ColumnValuesResponse, error) {
	var resp ColumnValuesResponse
	if err := c.do(ctx, RequestTypeColumnValues, req.Headers, req, &resp, ErrColumnValuesNotImplemented); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) do(ctx context.Context, path string, headers http.Header, reqBody any, out any, notImplErr error) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("schemads: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("schemads: failed to create HTTP request: %w", err)
	}

	for k, vals := range headers {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("schemads: request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.DefaultLogger.Warn("schemads: failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode == http.StatusNotImplemented {
		return notImplErr
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("schemads: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("schemads: failed to decode response: %w", err)
	}
	return nil
}
