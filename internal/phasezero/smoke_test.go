package phasezero

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

type fakeElasticSmokeAPI struct {
	images autodl.Page[autodl.Image]
	stock  autodl.GPUStock
}

func (f fakeElasticSmokeAPI) ElasticImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error) {
	return f.images, "images-request", nil
}

func (f fakeElasticSmokeAPI) ElasticGPUStock(context.Context, string, map[string]any) (autodl.GPUStock, string, error) {
	return f.stock, "stock-request", nil
}

func TestPrepareElasticSmokeSelectsVisibleCapacity(t *testing.T) {
	spec, err := PrepareElasticSmoke(context.Background(), fakeElasticSmokeAPI{
		images: autodl.Page[autodl.Image]{List: []autodl.Image{{UUID: "image-b"}, {UUID: "image-a"}}},
		stock: autodl.GPUStock{
			"CPU":      {Idle: 10},
			"RTX 4090": {Idle: 2},
			"RTX 3090": {Idle: 5},
			"busy":     {Idle: 0},
		},
	}, ElasticSmokeOptions{Region: "westDC2", PriceCeilingMilliPerHour: 1980})
	if err != nil {
		t.Fatal(err)
	}
	if spec.ImageUUID != "image-a" || !reflect.DeepEqual(spec.GPUNames, []string{"RTX 3090", "RTX 4090"}) {
		t.Fatalf("prepared smoke spec = %+v", spec)
	}
	if !spec.Minimal || spec.MaxRuntimeSeconds != 5 || spec.EstimatedMaximumSpendMilli() != 10 {
		t.Fatalf("smoke limits = %+v", spec)
	}
	if command := spec.probeCommand("probe", "/root/autodl-fs/probe"); !strings.Contains(command, "gemcp-autodl-smoke-ok") || strings.Contains(command, "nvidia-smi") {
		t.Fatalf("minimal command = %q", command)
	}
}

func TestPrepareElasticSmokeRejectsMissingCapacity(t *testing.T) {
	_, err := PrepareElasticSmoke(context.Background(), fakeElasticSmokeAPI{
		images: autodl.Page[autodl.Image]{List: []autodl.Image{{UUID: "image-a"}}},
		stock:  autodl.GPUStock{"CPU": {Idle: 10}},
	}, ElasticSmokeOptions{Region: "westDC2", PriceCeilingMilliPerHour: 1980})
	if err == nil {
		t.Fatal("PrepareElasticSmoke accepted inventory without an idle GPU")
	}
}
