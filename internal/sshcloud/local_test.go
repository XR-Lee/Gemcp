package sshcloud

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalDialRejectsNonLoopbackAndRunsCommands(t *testing.T) {
	ctx := context.Background()
	if _, _, err := LocalDial(ctx, Target{Host: "203.0.113.10", Port: 22, User: "ubuntu"}, Credential{Method: "password", Password: "local-process"}, ""); err == nil {
		t.Fatal("LocalDial accepted a public host")
	}
	conn, fingerprint, err := LocalDial(ctx, Target{Host: "127.0.0.1", Port: 22, User: "ubuntu"}, Credential{Method: "password", Password: "local-process"}, "")
	if err != nil || fingerprint != localProcessFingerprint {
		t.Fatalf("LocalDial() fingerprint=%q err=%v", fingerprint, err)
	}
	defer conn.Close()
	output, err := conn.Run(ctx, "uname -s", 4<<10)
	if err != nil || strings.TrimSpace(output) == "" {
		t.Fatalf("Run uname = %q err=%v", output, err)
	}
	dest := filepath.Join(t.TempDir(), "probe.txt")
	if err := conn.Upload(ctx, dest, strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || string(body) != "ok" {
		t.Fatalf("uploaded %q err=%v", body, err)
	}
}

func TestCreateLocalProcessFillsLoopbackDefaultsWithoutSecrets(t *testing.T) {
	service, _, tenant, project := newServiceFixture(t)
	service.config.LocalProcessEnabled = true
	ctx := context.Background()
	view, err := service.Create(ctx, tenant.ID, "agent:token", CreateInput{ProjectID: project.PublicID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if view.Host != "127.0.0.1" || view.Label != "local-cpu" || view.HostKeyFingerprint != localProcessFingerprint {
		t.Fatalf("defaults node=%+v", view)
	}
}

func TestEnsureForProjectCreatesLoopbackWhenLocalProcess(t *testing.T) {
	service, _, tenant, project := newServiceFixture(t)
	service.config.LocalProcessEnabled = true
	ctx := context.Background()
	result, err := service.EnsureForProject(ctx, tenant.ID, "agent:token", project.PublicID.String(), HostImage)
	if err != nil || result.EnvironmentName == "" || result.NodeID == "" {
		t.Fatalf("EnsureForProject()=%+v err=%v", result, err)
	}
}

func TestCreateLocalProcessProbesLoopbackWithoutSSH(t *testing.T) {
	service, _, tenant, project := newServiceFixture(t)
	service.config.LocalProcessEnabled = true
	ctx := context.Background()
	view, err := service.Create(ctx, tenant.ID, "agent:token", CreateInput{
		Label: "local-cpu", Host: "127.0.0.1", User: "ubuntu", AuthMethod: "password", Password: "local-process",
		ProjectID: project.PublicID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Host != "127.0.0.1" || view.HostKeyFingerprint != localProcessFingerprint {
		t.Fatalf("node=%+v", view)
	}
	if view.Status != "active" {
		t.Fatalf("status=%s inventory=%v", view.Status, view.Inventory)
	}
	if osName, _ := view.Inventory["os"].(string); strings.TrimSpace(osName) == "" {
		t.Fatalf("probe inventory missing os: %v", view.Inventory)
	}
}
