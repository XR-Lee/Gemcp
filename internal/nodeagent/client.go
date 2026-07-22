package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
)

const maxAPIResponseBytes = 4 << 20

type Client struct {
	origin    *url.URL
	http      *http.Client
	userAgent string
}

type ClientOption func(*Client)

func WithHTTPClient(client *http.Client) ClientOption {
	return func(target *Client) {
		if client != nil {
			target.http = client
		}
	}
}

func NewClient(origin, version string, options ...ClientOption) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(origin), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return nil, fmt.Errorf("Gemcp node origin must be a credential-free HTTPS origin")
	}
	client := &Client{
		origin: parsed,
		http: &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		userAgent: "Gemcp-Node/" + strings.TrimSpace(version),
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

func OriginFromSetupURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "/node/setup" || parsed.RawQuery != "" {
		return "", "", fmt.Errorf("node setup URL is invalid")
	}
	values, err := url.ParseQuery(parsed.Fragment)
	if err != nil || len(values) != 1 || len(values["code"]) != 1 || !strings.HasPrefix(values.Get("code"), "gne_") {
		return "", "", fmt.Errorf("node setup URL has an invalid code fragment")
	}
	origin := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
	return origin, values.Get("code"), nil
}

func (c *Client) Claim(ctx context.Context, code string, inventory nodeprotocol.Inventory) (nodeprotocol.EnrollmentClaimResponse, error) {
	return callAPI[nodeprotocol.EnrollmentClaimResponse](ctx, c, "/api/v1/node-enrollments/claim", "", nodeprotocol.EnrollmentClaimRequest{Code: code, Inventory: inventory})
}

func (c *Client) Sync(ctx context.Context, token string, request nodeprotocol.SyncRequest) (nodeprotocol.SyncResponse, error) {
	return callAPI[nodeprotocol.SyncResponse](ctx, c, "/api/v1/nodes/sync", token, request)
}

func (c *Client) DownloadSource(ctx context.Context, token, requestPath, filename string, maximum int64) error {
	if c == nil || c.origin == nil || c.http == nil || maximum <= 0 {
		return fmt.Errorf("Gemcp node source client is not initialized")
	}
	parsed, err := url.Parse(requestPath)
	if err != nil || !strings.HasPrefix(parsed.Path, "/api/v1/node-assignments/") || !strings.HasSuffix(parsed.Path, "/source") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("Gemcp node source path is invalid")
	}
	endpoint := c.origin.ResolveReference(&url.URL{Path: parsed.Path})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create Gemcp node source request: %w", err)
	}
	request.Header.Set("Accept", "application/gzip")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", c.userAgent)
	transferClient := *c.http
	transferClient.Timeout = 0
	response, err := transferClient.Do(request)
	if err != nil {
		return fmt.Errorf("download Gemcp source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Gemcp source download failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	compressedLimit := maximum + maximum/100 + (64 << 10)
	if compressedLimit < maximum || response.ContentLength > compressedLimit {
		return fmt.Errorf("Gemcp source archive exceeds its size limit")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("create source directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".source-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create source archive: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, compressedLimit+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("write source archive: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close source archive: %w", closeErr)
	}
	if written > compressedLimit {
		return fmt.Errorf("Gemcp source archive exceeds its size limit")
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("install source archive: %w", err)
	}
	return nil
}

func callAPI[T any](ctx context.Context, client *Client, requestPath, token string, body any) (T, error) {
	var zero T
	if client == nil || client.origin == nil || client.http == nil {
		return zero, fmt.Errorf("Gemcp node client is not initialized")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return zero, fmt.Errorf("encode Gemcp node request: %w", err)
	}
	endpoint := client.origin.ResolveReference(&url.URL{Path: requestPath})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return zero, fmt.Errorf("create Gemcp node request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", client.userAgent)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return zero, fmt.Errorf("call Gemcp node API: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil {
		return zero, fmt.Errorf("read Gemcp node API response: %w", err)
	}
	if len(payload) > maxAPIResponseBytes {
		return zero, fmt.Errorf("Gemcp node API response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error.Code != "" {
			return zero, fmt.Errorf("Gemcp node API rejected request: %s: %s", failure.Error.Code, failure.Error.Message)
		}
		return zero, fmt.Errorf("Gemcp node API rejected request with status %d", response.StatusCode)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return zero, fmt.Errorf("decode Gemcp node API response: %w", err)
	}
	return envelope.Data, nil
}
