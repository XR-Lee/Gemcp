package executioncmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const (
	ModeShell = "shell"
	ModeArgv  = "argv"

	MaxArguments = 256
	MaxBytes     = 64 << 10
)

var displaySafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

type Spec struct {
	Mode    string   `json:"mode"`
	Command string   `json:"command,omitempty"`
	Argv    []string `json:"argv,omitempty"`
}

func Shell(command string) (Spec, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > MaxBytes || strings.ContainsRune(command, 0) {
		return Spec{}, fmt.Errorf("command must contain 1 to %d bytes and no NUL characters", MaxBytes)
	}
	return Spec{Mode: ModeShell, Command: command, Argv: []string{}}, nil
}

func Argv(values []string) (Spec, error) {
	if len(values) == 0 || len(values) > MaxArguments {
		return Spec{}, fmt.Errorf("argv must contain 1 to %d arguments", MaxArguments)
	}
	result := make([]string, len(values))
	total := 0
	for index, value := range values {
		if value == "" && index == 0 {
			return Spec{}, fmt.Errorf("argv program must not be empty")
		}
		if strings.ContainsRune(value, 0) {
			return Spec{}, fmt.Errorf("argv must not contain NUL characters")
		}
		for _, character := range value {
			if unicode.IsControl(character) {
				return Spec{}, fmt.Errorf("argv must not contain control characters")
			}
		}
		total += len(value)
		if total > MaxBytes {
			return Spec{}, fmt.Errorf("argv exceeds %d bytes", MaxBytes)
		}
		result[index] = value
	}
	program := strings.ToLower(filepath.Base(result[0]))
	switch program {
	case "sh", "bash", "dash", "ash", "ksh", "zsh", "fish":
		return Spec{}, fmt.Errorf("shell interpreters require the Advanced command path")
	}
	return Spec{Mode: ModeArgv, Argv: result}, nil
}

func Validate(spec Spec) (Spec, error) {
	switch strings.TrimSpace(spec.Mode) {
	case "", ModeShell:
		if len(spec.Argv) != 0 {
			return Spec{}, fmt.Errorf("shell execution must not include argv")
		}
		return Shell(spec.Command)
	case ModeArgv:
		if strings.TrimSpace(spec.Command) != "" {
			return Spec{}, fmt.Errorf("argv execution must not include an authoritative command string")
		}
		return Argv(spec.Argv)
	default:
		return Spec{}, fmt.Errorf("execution mode must be shell or argv")
	}
}

func DisplayArgv(argv []string) string {
	parts := make([]string, len(argv))
	for index, value := range argv {
		if displaySafe.MatchString(value) {
			parts[index] = value
			continue
		}
		parts[index] = "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return strings.Join(parts, " ")
}
