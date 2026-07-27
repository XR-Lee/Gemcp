package executioncmd

import (
	"strings"
	"testing"
)

func TestArgvValidationAndDisplay(t *testing.T) {
	spec, err := Argv([]string{"python", "train.py", "--label", "value with spaces", "quote'value"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != ModeArgv || spec.Command != "" || DisplayArgv(spec.Argv) != "python train.py --label 'value with spaces' 'quote'\"'\"'value'" {
		t.Fatalf("argv spec = %+v, display = %q", spec, DisplayArgv(spec.Argv))
	}
	for _, values := range [][]string{{}, {"bash", "-lc", "echo unsafe"}, {"python", "bad\nvalue"}, {"", "arg"}} {
		if _, err := Argv(values); err == nil {
			t.Fatalf("Argv(%q) succeeded", values)
		}
	}
	oversized := []string{"python", strings.Repeat("x", MaxBytes)}
	if _, err := Argv(oversized); err == nil {
		t.Fatal("oversized argv succeeded")
	}
}

func TestShellCompatibilityIsSeparate(t *testing.T) {
	spec, err := Validate(Spec{Command: "python train.py"})
	if err != nil || spec.Mode != ModeShell {
		t.Fatalf("legacy shell spec = %+v, %v", spec, err)
	}
	if _, err := Validate(Spec{Mode: ModeArgv, Command: "python train.py", Argv: []string{"python", "train.py"}}); err == nil {
		t.Fatal("argv spec accepted an authoritative command string")
	}
}
