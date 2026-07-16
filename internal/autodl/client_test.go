package autodl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "test-secret-token", WithInsecureHTTP(), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func TestWalletBalance(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "test-secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.URL.Path != "/api/v1/dev/wallet/balance" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Success","data":{"assets":1200,"accumulate":3400,"voucher_balance":50},"msg":"","request_id":"req-1"}`))
	}))

	balance, requestID, err := client.WalletBalance(context.Background())
	if err != nil {
		t.Fatalf("WalletBalance() error = %v", err)
	}
	if balance.Assets != 1200 || balance.Accumulate != 3400 || requestID != "req-1" {
		t.Fatalf("unexpected response: %+v request_id=%s", balance, requestID)
	}
}

func TestElasticGPUStockNormalization(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"Success","data":[{"RTX 4090":{"idle_gpu_num":3,"total_gpu_num":9}},{"RTX 3090":{"idle_gpu_num":1,"total_gpu_num":5}}],"msg":""}`))
	}))
	stock, _, err := client.ElasticGPUStock(context.Background(), "westDC2", nil)
	if err != nil {
		t.Fatalf("ElasticGPUStock() error = %v", err)
	}
	if stock["RTX 4090"].Idle != 3 || stock["RTX 3090"].Total != 5 {
		t.Fatalf("unexpected stock: %+v", stock)
	}
}

func TestProviderErrorDoesNotLeakToken(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"Unauthorized","data":null,"msg":"bad token","request_id":"req-denied"}`))
	}))
	_, _, err := client.WalletBalance(context.Background())
	if err == nil {
		t.Fatal("WalletBalance() succeeded")
	}
	if strings.Contains(err.Error(), "test-secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.RequestID != "req-denied" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdempotentReadRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"Unavailable","data":null,"msg":"retry"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":"Success","data":{"assets":1,"accumulate":2,"voucher_balance":0},"msg":""}`))
	}))
	if _, _, err := client.WalletBalance(context.Background()); err != nil {
		t.Fatalf("WalletBalance() error = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestContainerResponseDropsAccessCredentials(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"Success","data":{"list":[{"uuid":"container-1","status":"running","info":{"root_password":"must-not-escape","ssh_command":"ssh root@example"}}],"page_index":1,"page_size":10,"max_page":1},"msg":""}`))
	}))
	containers, _, err := client.ElasticContainers(context.Background(), "deployment-1", 1, 10)
	if err != nil {
		t.Fatalf("ElasticContainers() error = %v", err)
	}
	encoded, err := json.Marshal(containers)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "must-not-escape") || strings.Contains(string(encoded), "ssh root@example") {
		t.Fatalf("container response leaked access credentials: %s", encoded)
	}
}

func TestClientRejectsInsecureProviderURL(t *testing.T) {
	if _, err := NewClient("http://api.autodl.example", "token"); err == nil {
		t.Fatal("NewClient() accepted insecure URL")
	}
}
