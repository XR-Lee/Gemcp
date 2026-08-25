package sshcloud

import (
	"net"
	"strconv"
	"strings"
	"unicode"
)

func applySSHTarget(input *CreateInput) error {
	if input == nil {
		return invalid("Cloud SSH registration is required")
	}
	if parsed := parseSSHTarget(input.SSH); parsed != nil {
		if input.Host == "" {
			input.Host = parsed.Host
		}
		if input.Port == 0 {
			input.Port = parsed.Port
		}
		if input.User == "" {
			input.User = parsed.User
		}
		if input.Password == "" && parsed.Password != "" {
			input.Password = parsed.Password
			if input.AuthMethod == "" {
				input.AuthMethod = "password"
			}
		}
	}
	if input.AuthMethod == "" {
		if strings.TrimSpace(input.PrivateKey) != "" {
			input.AuthMethod = "private_key"
		} else {
			input.AuthMethod = "password"
		}
	}
	if strings.TrimSpace(input.Label) == "" && input.Host != "" {
		input.Label = suggestedSSHLabel(input.Host)
	}
	return nil
}

func parseSSHTarget(raw string) *parsedSSH {
	text := strings.ReplaceAll(raw, "\u00a0", " ")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	password := extractSSHPassword(text)
	line := findSSHLine(text)
	if line == "" {
		return nil
	}
	target := parseSSHLine(line)
	if target == nil {
		return nil
	}
	if password != "" {
		target.Password = password
	}
	return target
}

type parsedSSH struct {
	Target
	Password string
}

func extractSSHPassword(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		for _, prefix := range []string{"password:", "password：", "passwd:", "pwd:", "密码:", "密码：", "口令:", "口令："} {
			if strings.HasPrefix(lower, strings.ToLower(prefix[:len(prefix)-1])) {
				_, value, ok := strings.Cut(trimmed, ":")
				if !ok {
					_, value, ok = strings.Cut(trimmed, "：")
				}
				if ok {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return ""
}

func findSSHLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "ssh ") || strings.HasPrefix(lower, "ssh:") || strings.Contains(line, "@") {
			return line
		}
	}
	return strings.TrimSpace(text)
}

func parseSSHLine(line string) *parsedSSH {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	if strings.EqualFold(fields[0], "ssh") || strings.EqualFold(fields[0], "sftp") || strings.EqualFold(fields[0], "scp") {
		fields = fields[1:]
	}
	user, host := "", ""
	port := defaultSSHPort
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		switch {
		case field == "-p" || field == "-P":
			if i+1 < len(fields) {
				if parsed, err := strconv.Atoi(fields[i+1]); err == nil {
					port = parsed
				}
				i++
			}
		case strings.HasPrefix(field, "-p") && len(field) > 2:
			if parsed, err := strconv.Atoi(field[2:]); err == nil {
				port = parsed
			}
		case strings.HasPrefix(field, "-"):
			if len(field) == 2 && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i++
			}
		case strings.Contains(field, "@"):
			user, host, _ = strings.Cut(field, "@")
			if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
				name, rawPort, ok := strings.Cut(host, ":")
				if ok {
					if parsed, err := strconv.Atoi(rawPort); err == nil {
						host, port = name, parsed
					}
				}
			}
		}
	}
	if user == "" || host == "" {
		return nil
	}
	return &parsedSSH{Target: Target{Host: host, Port: port, User: user}}
}

func suggestedSSHLabel(host string) string {
	host = strings.Trim(host, "[]")
	if first, _, ok := strings.Cut(host, "."); ok && first != "" {
		return first
	}
	return host
}

func normalizeTarget(host string, port int, user string) (Target, error) {
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if port == 0 {
		port = defaultSSHPort
	}
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "/@ \t") || strings.Contains(host, ":") {
		return Target{}, invalid("SSH host must be a hostname or IP without credentials or a port")
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return Target{}, invalid("Cloud SSH targets cannot be loopback hosts")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return Target{}, invalid("Cloud SSH targets cannot be loopback, link-local, or unspecified addresses")
		}
	} else {
		for _, character := range host {
			if unicode.IsControl(character) || unicode.IsSpace(character) {
				return Target{}, invalid("SSH host contains an invalid character")
			}
		}
	}
	if port < 1 || port > 65535 {
		return Target{}, invalid("SSH port must be between 1 and 65535")
	}
	if user == "" || len(user) > 64 || strings.ContainsAny(user, " \t/@:") {
		return Target{}, invalid("SSH user is required")
	}
	for _, character := range user {
		if unicode.IsControl(character) {
			return Target{}, invalid("SSH user contains a control character")
		}
	}
	return Target{Host: host, Port: port, User: user}, nil
}

func (t Target) Address() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

func normalizeLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 120 {
		return "", invalid("label is required")
	}
	for _, character := range label {
		if unicode.IsControl(character) {
			return "", invalid("label contains a control character")
		}
	}
	return label, nil
}
