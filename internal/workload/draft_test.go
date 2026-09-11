package workload

import (
	"strings"
	"testing"
)

func TestDraftFromArgvRoundTrip(t *testing.T) {
	manifest, raw, err := DraftFromArgv(DraftInput{
		Name:             "oneshot-smoke",
		Argv:             []string{"python", "tools/smoke.py", "--label", "value with spaces"},
		RuntimePreset:    "smoke",
		Dataset:          "scanobjectnn-objbg",
		WorkingDirectory: "tools",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "oneshot-smoke:") || !strings.Contains(string(raw), "python") {
		t.Fatalf("draft yaml = %s", raw)
	}
	resolved, err := Resolve(manifest, "oneshot-smoke", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"python", "tools/smoke.py", "--label", "value with spaces"}
	if strings.Join(resolved.Argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %#v", resolved.Argv)
	}
	if resolved.RuntimePreset != "smoke" || resolved.Dataset != "scanobjectnn-objbg" || resolved.WorkingDirectory != "tools" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if _, err := Parse(raw); err != nil {
		t.Fatal(err)
	}
}

func TestDraftFromArgvRejectsInvalidNameAndArgv(t *testing.T) {
	if _, _, err := DraftFromArgv(DraftInput{Name: "OneShot", Argv: []string{"python"}}); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, _, err := DraftFromArgv(DraftInput{Name: "oneshot", Argv: nil}); err == nil {
		t.Fatal("empty argv accepted")
	}
	if _, _, err := DraftFromArgv(DraftInput{Name: "oneshot", Argv: []string{"python"}, RuntimePreset: "provision"}); err == nil {
		t.Fatal("provision preset accepted")
	}
}

func TestValidName(t *testing.T) {
	if !ValidName("objbg-smoke") || ValidName("-bad") || ValidName("Bad") {
		t.Fatal("ValidName contract drifted")
	}
}
