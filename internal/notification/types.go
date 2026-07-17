package notification

import "time"

type SettingView struct {
	Configured         bool       `json:"configured"`
	Enabled            bool       `json:"enabled"`
	Host               string     `json:"host,omitempty"`
	Port               int        `json:"port,omitempty"`
	TLSMode            string     `json:"tls_mode,omitempty"`
	Username           string     `json:"username,omitempty"`
	PasswordConfigured bool       `json:"password_configured"`
	FromAddress        string     `json:"from_address,omitempty"`
	Recipients         []string   `json:"recipients"`
	Status             string     `json:"status,omitempty"`
	LastTestedAt       *time.Time `json:"last_tested_at,omitempty"`
	LastError          *string    `json:"last_error,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

type ConfigureInput struct {
	Enabled       bool     `json:"enabled"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	TLSMode       string   `json:"tls_mode"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	ClearPassword bool     `json:"clear_password"`
	FromAddress   string   `json:"from_address"`
	Recipients    []string `json:"recipients"`
}

type NotificationView struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	Severity      string     `json:"severity"`
	Subject       string     `json:"subject"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LastError     *string    `json:"last_error,omitempty"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type EnqueueInput struct {
	TenantID int
	DedupKey string
	Kind     string
	Severity string
	Subject  string
	Body     string
}
