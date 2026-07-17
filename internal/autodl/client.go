package autodl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL  = "https://api.autodl.com"
	PrivateBaseURL  = "https://private.autodl.com"
	maxResponseSize = 4 << 20
)

type Client struct {
	baseURL   *url.URL
	token     string
	http      *http.Client
	userAgent string
	insecure  bool
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

func WithUserAgent(userAgent string) Option {
	return func(c *Client) { c.userAgent = strings.TrimSpace(userAgent) }
}

// WithInsecureHTTP is intended only for local contract tests.
func WithInsecureHTTP() Option {
	return func(c *Client) { c.insecure = true }
}

func NewClient(baseURL, token string, options ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse AutoDL base URL: %w", err)
	}
	client := &Client{
		baseURL: parsed,
		token:   strings.TrimSpace(token),
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		userAgent: "Gemcp/dev",
	}
	for _, option := range options {
		option(client)
	}
	if client.token == "" {
		return nil, fmt.Errorf("AutoDL developer token is required")
	}
	if client.baseURL.Scheme != "https" && !client.insecure {
		return nil, fmt.Errorf("AutoDL base URL must use https")
	}
	if client.baseURL.Host == "" {
		return nil, fmt.Errorf("AutoDL base URL must include a host")
	}
	if client.baseURL.User != nil || client.baseURL.RawQuery != "" || client.baseURL.Fragment != "" {
		return nil, fmt.Errorf("AutoDL base URL must not contain credentials, query parameters, or fragments")
	}
	return client, nil
}

type requestOptions struct {
	idempotent bool
}

func doJSON[T any](ctx context.Context, client *Client, method, requestPath string, body any, options requestOptions) (T, string, error) {
	var zero T
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return zero, "", fmt.Errorf("encode AutoDL request: %w", err)
		}
	}

	attempts := 1
	if options.idempotent {
		attempts = 3
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		result, requestID, retry, callErr := doJSONOnce[T](ctx, client, method, requestPath, encoded)
		if callErr == nil {
			return result, requestID, nil
		}
		if !retry || attempt == attempts {
			return zero, requestID, callErr
		}
		delay := time.Duration(attempt*250) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, requestID, ctx.Err()
		case <-timer.C:
		}
	}
	return zero, "", fmt.Errorf("AutoDL request exhausted retries")
}

func doJSONOnce[T any](ctx context.Context, client *Client, method, requestPath string, encoded []byte) (T, string, bool, error) {
	var zero T
	endpoint := client.baseURL.ResolveReference(&url.URL{Path: requestPath})
	var bodyReader io.Reader
	if encoded != nil {
		bodyReader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bodyReader)
	if err != nil {
		return zero, "", false, fmt.Errorf("create AutoDL request: %w", err)
	}
	request.Header.Set("Authorization", client.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)
	if encoded != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.http.Do(request)
	if err != nil {
		return zero, "", true, fmt.Errorf("call AutoDL API: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return zero, "", false, fmt.Errorf("read AutoDL response: %w", err)
	}
	if len(payload) > maxResponseSize {
		return zero, "", false, fmt.Errorf("AutoDL response exceeds %d bytes", maxResponseSize)
	}

	var wrapped envelope
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return zero, "", retry, &ProviderError{HTTPStatus: response.StatusCode, Code: "INVALID_RESPONSE", Message: "provider returned non-JSON or malformed JSON"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || wrapped.Code != SuccessCode {
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		message := wrapped.Message
		if client.token != "" {
			message = strings.ReplaceAll(message, client.token, "[REDACTED]")
		}
		return zero, wrapped.RequestID, retry, &ProviderError{
			HTTPStatus: response.StatusCode,
			Code:       wrapped.Code,
			Message:    message,
			RequestID:  wrapped.RequestID,
		}
	}

	var result T
	if len(wrapped.Data) == 0 || bytes.Equal(wrapped.Data, []byte("null")) {
		return result, wrapped.RequestID, false, nil
	}
	if err := json.Unmarshal(wrapped.Data, &result); err != nil {
		return zero, wrapped.RequestID, false, fmt.Errorf("decode AutoDL response data: %w", err)
	}
	return result, wrapped.RequestID, false, nil
}
