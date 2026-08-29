package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvAppliesMissingKeysOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "GEMCP_DOTENV_TEST_A=from-file\nexport GEMCP_DOTENV_TEST_B=quoted\n# comment\nGEMCP_DOTENV_TEST_C=keep-existing\n"
	if err := os.WriteFile(".env", []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEMCP_ENV_FILE", "")
	t.Setenv("GEMCP_DOTENV_TEST_A", "")
	t.Setenv("GEMCP_DOTENV_TEST_B", "")
	t.Setenv("GEMCP_DOTENV_TEST_C", "already-set")
	if err := os.Unsetenv("GEMCP_DOTENV_TEST_A"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("GEMCP_DOTENV_TEST_B"); err != nil {
		t.Fatal(err)
	}

	if err := LoadDotEnv(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GEMCP_DOTENV_TEST_A"); got != "from-file" {
		t.Fatalf("A = %q", got)
	}
	if got := os.Getenv("GEMCP_DOTENV_TEST_B"); got != "quoted" {
		t.Fatalf("B = %q", got)
	}
	if got := os.Getenv("GEMCP_DOTENV_TEST_C"); got != "already-set" {
		t.Fatalf("C overwritten: %q", got)
	}
}

func TestLoadDotEnvMissingDefaultIsOK(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GEMCP_ENV_FILE", "")
	if err := LoadDotEnv(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDotEnvExplicitMissingFails(t *testing.T) {
	t.Setenv("GEMCP_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	if err := LoadDotEnv(); err == nil {
		t.Fatal("LoadDotEnv() accepted a missing GEMCP_ENV_FILE")
	}
}

func TestParseDotEnvLine(t *testing.T) {
	key, value, skip, err := parseDotEnvLine(`GEMCP_PUBLIC_URL="http://127.0.0.1:8080" # local`)
	if err != nil || skip || key != "GEMCP_PUBLIC_URL" || value != "http://127.0.0.1:8080" {
		t.Fatalf("quoted = %q %q skip=%v err=%v", key, value, skip, err)
	}
	key, value, skip, err = parseDotEnvLine("GEMCP_LOG_LEVEL=info # comment")
	if err != nil || skip || key != "GEMCP_LOG_LEVEL" || value != "info" {
		t.Fatalf("comment = %q %q skip=%v err=%v", key, value, skip, err)
	}
	_, _, skip, err = parseDotEnvLine("# ignored")
	if err != nil || !skip {
		t.Fatalf("comment line skip=%v err=%v", skip, err)
	}
	if _, _, _, err := parseDotEnvLine("not-a-pair"); err == nil {
		t.Fatal("accepted a line without =")
	}
}
