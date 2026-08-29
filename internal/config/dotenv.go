package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// LoadDotEnv applies KEY=VALUE pairs from a dotenv file into the process
// environment. Existing variables are never overwritten.
//
// Path resolution:
//   - GEMCP_ENV_FILE, when set, is loaded and must exist
//   - otherwise .env in the current working directory is loaded if present
func LoadDotEnv() error {
	if explicit := strings.TrimSpace(os.Getenv("GEMCP_ENV_FILE")); explicit != "" {
		return loadDotEnvFile(explicit, true)
	}
	return loadDotEnvFile(".env", false)
}

func loadDotEnvFile(path string, required bool) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return fmt.Errorf("open environment file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		key, value, skip, parseErr := parseDotEnvLine(scanner.Text())
		if parseErr != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNumber, parseErr)
		}
		if skip {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNumber, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read environment file %s: %w", path, err)
	}
	return nil
}

func parseDotEnvLine(raw string) (key, value string, skip bool, err error) {
	line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", true, nil
	}
	if rest, found := strings.CutPrefix(line, "export "); found {
		line = strings.TrimSpace(rest)
	}
	key, rawValue, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false, fmt.Errorf("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if key == "" || !isDotEnvKey(key) {
		return "", "", false, fmt.Errorf("invalid environment variable name %q", key)
	}
	value, err = unquoteDotEnvValue(strings.TrimSpace(rawValue))
	if err != nil {
		return "", "", false, err
	}
	return key, value, false, nil
}

func isDotEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func unquoteDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		raw = stripTrailingDotEnvComment(raw)
		if len(raw) < 2 || !strings.HasSuffix(raw, `"`) {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		inner := raw[1 : len(raw)-1]
		var builder strings.Builder
		escaped := false
		for _, r := range inner {
			if escaped {
				switch r {
				case 'n':
					builder.WriteByte('\n')
				case 'r':
					builder.WriteByte('\r')
				case 't':
					builder.WriteByte('\t')
				default:
					builder.WriteRune(r)
				}
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			builder.WriteRune(r)
		}
		if escaped {
			return "", fmt.Errorf("unterminated escape in double-quoted value")
		}
		return builder.String(), nil
	case '\'':
		raw = stripTrailingDotEnvComment(raw)
		if len(raw) < 2 || !strings.HasSuffix(raw, "'") {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	default:
		return stripTrailingDotEnvComment(raw), nil
	}
}

func stripTrailingDotEnvComment(raw string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if escaped {
			escaped = false
			continue
		}
		switch {
		case c == '\\' && inDouble:
			escaped = true
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble && (i == 0 || unicode.IsSpace(rune(raw[i-1]))):
			return strings.TrimSpace(raw[:i])
		}
	}
	return strings.TrimSpace(raw)
}
