package servicehealth

import (
	"context"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	_ "github.com/mattn/go-sqlite3"
)

func TestBeatUpsertsInstanceAndLatestReturnsNewestRole(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:servicehealth?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()

	if err := Beat(ctx, client, serviceheartbeat.RoleScheduler, "controlplane-a", map[string]any{"tick": 1}); err != nil {
		t.Fatalf("first Beat: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := Beat(ctx, client, serviceheartbeat.RoleScheduler, "controlplane-a", map[string]any{"tick": 2}); err != nil {
		t.Fatalf("second Beat: %v", err)
	}
	if err := Beat(ctx, client, serviceheartbeat.RoleScheduler, "controlplane-b", map[string]any{"tick": 3}); err != nil {
		t.Fatalf("third Beat: %v", err)
	}

	if count, err := client.ServiceHeartbeat.Query().Count(ctx); err != nil || count != 2 {
		t.Fatalf("heartbeat count = %d, %v; want 2, nil", count, err)
	}
	latest, err := Latest(ctx, client, serviceheartbeat.RoleScheduler)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest == nil || latest.InstanceID != "controlplane-b" || latest.Status != "running" || latest.Metadata["tick"] != float64(3) {
		t.Fatalf("Latest = %#v", latest)
	}
	missing, err := Latest(ctx, client, serviceheartbeat.RoleWatchdog)
	if err != nil || missing != nil {
		t.Fatalf("missing Latest = %#v, %v; want nil, nil", missing, err)
	}
}
