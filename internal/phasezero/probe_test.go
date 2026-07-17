package phasezero

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

func TestPrivateReadProbeSkipsWalletAndReadsPrivateContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/dev/image/private/list":
			_, _ = w.Write([]byte(`{"code":"Success","data":{"list":[],"page_index":1,"page_size":100},"msg":"","request_id":"req-private-images"}`))
		case "/api/v2/image/list":
			_, _ = w.Write([]byte(`{"code":"Success","data":{"list":[{"image_uuid":"base-image-1","name":"torch","cuda_version":"11.8"}],"page_index":1,"page_size":100},"msg":"","request_id":"req-system-images"}`))
		case "/api/v1/dev/machine/gpu_stock":
			if r.Method != http.MethodGet {
				t.Fatalf("GPU stock method = %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"code":"Success","data":{"NVIDIA GeForce RTX 3090":{"idle_gpu_num":2,"total_gpu_num":9}},"msg":"","request_id":"req-stock"}`))
		case "/api/v1/dev/deployment/list":
			_, _ = w.Write([]byte(`{"code":"Success","data":{"list":[],"page_index":1,"page_size":100},"msg":"","request_id":"req-deployments"}`))
		case "/api/v1/dev/wallet/balance":
			t.Fatal("Private Cloud probe called the unsupported wallet endpoint")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := autodl.NewClient(server.URL, "test-token", autodl.WithInsecureHTTP(), autodl.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	report, err := RunReadProbe(context.Background(), client, "private", server.URL, "")
	if err != nil {
		t.Fatalf("RunReadProbe() error = %v", err)
	}
	if report.Wallet != nil {
		t.Fatalf("Private Cloud report contains a wallet: %+v", report.Wallet)
	}
	if len(report.SystemImages) != 1 || report.SystemImages[0].UUID != "base-image-1" {
		t.Fatalf("unexpected system images: %+v", report.SystemImages)
	}
	if len(report.GPUStock) != 1 || report.GPUStock[0].Idle != 2 {
		t.Fatalf("unexpected GPU stock: %+v", report.GPUStock)
	}
}
