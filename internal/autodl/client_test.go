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
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/dev/machine/region/gpu_stock" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["region_sign"] != "westDC2" {
			t.Fatalf("region_sign = %v", body["region_sign"])
		}
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

func TestPrivateElasticGPUStockContract(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/dev/machine/gpu_stock" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":"Success","data":{"RTX 4090":{"idle_gpu_num":7,"total_gpu_num":12}},"msg":""}`))
	}))
	stock, _, err := client.PrivateElasticGPUStock(context.Background())
	if err != nil {
		t.Fatalf("PrivateElasticGPUStock() error = %v", err)
	}
	if stock["RTX 4090"].Idle != 7 || stock["RTX 4090"].Total != 12 {
		t.Fatalf("unexpected stock: %+v", stock)
	}
}

func TestPrivateSystemImagesContract(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/image/list" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":"Success","data":{"list":[{"image_uuid":"base-image-1","name":"torch","cuda_version":"11.8","chip_corp":"nvidia","cpu_arch":"x86"}],"page_index":1,"page_size":100},"msg":""}`))
	}))
	images, _, err := client.PrivateSystemImages(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("PrivateSystemImages() error = %v", err)
	}
	if len(images.List) != 1 || images.List[0].UUID != "base-image-1" || images.List[0].CUDAVersion != "11.8" {
		t.Fatalf("unexpected images: %+v", images)
	}
}

func TestPrivateElasticDeploymentContract(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/dev/deployment" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		template, ok := body["container_template"].(map[string]any)
		if !ok {
			t.Fatalf("container_template = %#v", body["container_template"])
		}
		if template["cuda_v"] != float64(118) {
			t.Fatalf("cuda_v = %v", template["cuda_v"])
		}
		if _, exists := template["dc_list"]; exists {
			t.Fatalf("private request contains dc_list: %#v", template)
		}
		if _, exists := template["cuda_v_from"]; exists {
			t.Fatalf("private request contains cuda_v_from: %#v", template)
		}
		_, _ = w.Write([]byte(`{"code":"Success","data":{"deployment_uuid":"deployment-private"},"msg":""}`))
	}))
	input := PrivateElasticDeploymentCreate{
		Name:           "private-probe",
		DeploymentType: "Job",
		ReplicaNum:     1,
		ParallelismNum: 1,
		ContainerTemplate: PrivateElasticContainerTemplate{
			CUDAVersion:    118,
			GPUNames:       []string{"RTX 4090"},
			GPUNum:         1,
			MemoryFromGB:   1,
			MemoryToGB:     64,
			CPUFrom:        1,
			CPUTo:          32,
			PriceFromMilli: 10,
			PriceToMilli:   3000,
			ImageUUID:      "image-test",
			Command:        "true",
		},
	}
	created, _, err := client.CreatePrivateElasticDeployment(context.Background(), input)
	if err != nil {
		t.Fatalf("CreatePrivateElasticDeployment() error = %v", err)
	}
	if created.DeploymentUUID != "deployment-private" {
		t.Fatalf("deployment UUID = %q", created.DeploymentUUID)
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

func TestProCreateAndSaveFailClosedOnProviderError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Error","msg":"not implemented","data":null}`))
	}))
	if _, _, err := client.ProCreateInstance(context.Background(), ProInstanceCreate{Name: "bake", ImageUUID: "image-1"}); err == nil {
		t.Fatal("ProCreateInstance succeeded on Provider error")
	}
	if _, err := client.ProStopInstance(context.Background(), "instance-1"); err == nil {
		t.Fatal("ProStopInstance succeeded on Provider error")
	}
	if _, _, err := client.ProSaveImage(context.Background(), ProImageSave{InstanceUUID: "instance-1", Name: "bake"}); err == nil {
		t.Fatal("ProSaveImage succeeded on Provider error")
	}
}

func TestProCreateAndSaveSuccessContract(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/dev/instance/pro/create":
			_, _ = w.Write([]byte(`{"code":"Success","data":{"uuid":"pro-1","name":"bake","status":"running"},"msg":""}`))
		case "/api/v1/dev/instance/pro/stop":
			_, _ = w.Write([]byte(`{"code":"Success","data":{},"msg":""}`))
		case "/api/v1/dev/instance/pro/image/save":
			_, _ = w.Write([]byte(`{"code":"Success","data":{"image_uuid":"image-baked12345","image_name":"bake"},"msg":""}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	instance, _, err := client.ProCreateInstance(context.Background(), ProInstanceCreate{Name: "bake", ImageUUID: "image-1", Command: "python -m pip install --user -r requirements.gemcp.txt"})
	if err != nil || instance.UUID != "pro-1" {
		t.Fatalf("ProCreateInstance = %+v, %v", instance, err)
	}
	if _, err := client.ProStopInstance(context.Background(), instance.UUID); err != nil {
		t.Fatalf("ProStopInstance error = %v", err)
	}
	image, _, err := client.ProSaveImage(context.Background(), ProImageSave{InstanceUUID: instance.UUID, Name: "bake"})
	if err != nil || image.UUID != "image-baked12345" {
		t.Fatalf("ProSaveImage = %+v, %v", image, err)
	}
}
