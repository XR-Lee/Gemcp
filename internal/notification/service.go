package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	entnotification "github.com/XR-Lee/Gemcp/ent/notification"
	"github.com/XR-Lee/Gemcp/ent/notificationsetting"
	"github.com/XR-Lee/Gemcp/ent/tenant"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

const smtpPasswordAADPrefix = "gemcp:smtp-password:v1:"

var (
	ErrNotConfigured = errors.New("SMTP notifications are not configured")
	ErrInvalidInput  = errors.New("invalid notification configuration")
	hostPattern      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,253}[A-Za-z0-9])?$`)
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type Service struct {
	client *ent.Client
	box    *secrets.Box
	now    func() time.Time
}

func NewService(client *ent.Client, box *secrets.Box) *Service {
	return &Service{client: client, box: box, now: time.Now}
}

func (s *Service) Setting(ctx context.Context, tenantID int) (SettingView, error) {
	record, err := s.client.NotificationSetting.Query().Where(notificationsetting.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return SettingView{Recipients: []string{}}, nil
	}
	if err != nil {
		return SettingView{}, err
	}
	return settingView(record), nil
}

func (s *Service) Configure(ctx context.Context, tenantID int, actorID string, input ConfigureInput) (SettingView, error) {
	normalized, err := validateConfigure(input)
	if err != nil {
		return SettingView{}, err
	}
	tenantRecord, err := s.client.Tenant.Query().Where(tenant.IDEQ(tenantID)).Only(ctx)
	if err != nil {
		return SettingView{}, err
	}
	candidateCiphertext := ""
	if normalized.Password != "" {
		plaintext := []byte(normalized.Password)
		candidateCiphertext, err = s.box.Encrypt(plaintext, smtpPasswordAADPrefix+tenantRecord.PublicID.String())
		for index := range plaintext {
			plaintext[index] = 0
		}
		if err != nil {
			return SettingView{}, fmt.Errorf("encrypt SMTP password: %w", err)
		}
	}

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return SettingView{}, err
	}
	defer tx.Rollback()
	var record *ent.NotificationSetting
	current, queryErr := tx.NotificationSetting.Query().Where(notificationsetting.TenantIDEQ(tenantID)).Only(ctx)
	switch {
	case ent.IsNotFound(queryErr):
		if normalized.Username != "" && candidateCiphertext == "" {
			return SettingView{}, &ValidationError{Message: "SMTP password is required when a username is configured"}
		}
		builder := tx.NotificationSetting.Create().SetTenantID(tenantID).SetEnabled(normalized.Enabled).
			SetHost(normalized.Host).SetPort(normalized.Port).SetTLSMode(notificationsetting.TLSMode(normalized.TLSMode)).
			SetUsername(normalized.Username).SetFromAddress(normalized.FromAddress).SetRecipients(normalized.Recipients)
		if candidateCiphertext != "" {
			builder.SetPasswordCiphertext(candidateCiphertext)
		}
		record, err = builder.Save(ctx)
	case queryErr != nil:
		err = queryErr
	default:
		passwordCiphertext := current.PasswordCiphertext
		if normalized.ClearPassword {
			passwordCiphertext = ""
		} else if candidateCiphertext != "" {
			passwordCiphertext = candidateCiphertext
		}
		if normalized.Username != "" && passwordCiphertext == "" {
			return SettingView{}, &ValidationError{Message: "SMTP password is required when a username is configured"}
		}
		if normalized.Username == "" && passwordCiphertext != "" {
			return SettingView{}, &ValidationError{Message: "clear the stored SMTP password when removing the username"}
		}
		builder := tx.NotificationSetting.UpdateOneID(current.ID).SetEnabled(normalized.Enabled).
			SetHost(normalized.Host).SetPort(normalized.Port).SetTLSMode(notificationsetting.TLSMode(normalized.TLSMode)).
			SetUsername(normalized.Username).SetFromAddress(normalized.FromAddress).SetRecipients(normalized.Recipients).
			SetStatus(notificationsetting.StatusReady).ClearLastError()
		if normalized.ClearPassword {
			builder.ClearPasswordCiphertext()
		} else if candidateCiphertext != "" {
			builder.SetPasswordCiphertext(candidateCiphertext)
		}
		record, err = builder.Save(ctx)
	}
	if err != nil {
		return SettingView{}, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("notification.smtp_configured").SetTargetType("notification_setting").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"enabled": normalized.Enabled, "host": normalized.Host, "port": normalized.Port, "tls_mode": normalized.TLSMode, "recipient_count": len(normalized.Recipients)}).Save(ctx); err != nil {
		return SettingView{}, err
	}
	if err := tx.Commit(); err != nil {
		return SettingView{}, err
	}
	return settingView(record), nil
}

func (s *Service) EnqueueTest(ctx context.Context, tenantID int, actorID string) (NotificationView, error) {
	setting, err := s.client.NotificationSetting.Query().Where(notificationsetting.TenantIDEQ(tenantID), notificationsetting.EnabledEQ(true)).Only(ctx)
	if ent.IsNotFound(err) {
		return NotificationView{}, ErrNotConfigured
	}
	if err != nil {
		return NotificationView{}, err
	}
	now := s.now().UTC()
	record, err := s.client.Notification.Create().SetTenantID(tenantID).SetDedupKey("smtp-test:" + now.Format("20060102T150405.000000000Z")).
		SetKind("smtp_test").SetSeverity(entnotification.SeverityInfo).SetSubject("Gemcp SMTP test").
		SetBody("Gemcp successfully queued this SMTP test for delivery.").SetNextAttemptAt(now).Save(ctx)
	if err != nil {
		return NotificationView{}, err
	}
	_, _ = setting.Update().SetStatus(notificationsetting.StatusReady).ClearLastError().Save(ctx)
	_, err = s.client.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("notification.smtp_test_queued").SetTargetType("notification").SetTargetID(record.PublicID.String()).Save(ctx)
	if err != nil {
		return NotificationView{}, err
	}
	return notificationView(record), nil
}

func (s *Service) List(ctx context.Context, tenantID, limit int) ([]NotificationView, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, &ValidationError{Message: "notification limit must be between 1 and 200"}
	}
	records, err := s.client.Notification.Query().Where(entnotification.TenantIDEQ(tenantID)).
		Order(ent.Desc(entnotification.FieldCreatedAt)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]NotificationView, 0, len(records))
	for _, record := range records {
		result = append(result, notificationView(record))
	}
	return result, nil
}

func Enqueue(ctx context.Context, client *ent.NotificationClient, input EnqueueInput) error {
	input.DedupKey = strings.TrimSpace(input.DedupKey)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Body = strings.TrimSpace(input.Body)
	if input.TenantID <= 0 || input.DedupKey == "" || len(input.DedupKey) > 180 || input.Kind == "" || len(input.Kind) > 80 || input.Subject == "" || len(input.Subject) > 200 || input.Body == "" || len(input.Body) > (16<<10) {
		return ErrInvalidInput
	}
	severity := entnotification.Severity(input.Severity)
	if err := entnotification.SeverityValidator(severity); err != nil {
		return ErrInvalidInput
	}
	exists, err := client.Query().Where(entnotification.TenantIDEQ(input.TenantID), entnotification.DedupKeyEQ(input.DedupKey)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	_, err = client.Create().SetTenantID(input.TenantID).SetDedupKey(input.DedupKey).SetKind(input.Kind).
		SetSeverity(severity).SetSubject(input.Subject).SetBody(input.Body).Save(ctx)
	return err
}

func validateConfigure(input ConfigureInput) (ConfigureInput, error) {
	input.Host = strings.ToLower(strings.TrimSpace(input.Host))
	input.Username = strings.TrimSpace(input.Username)
	input.FromAddress = strings.TrimSpace(input.FromAddress)
	input.TLSMode = strings.ToLower(strings.TrimSpace(input.TLSMode))
	if input.Host == "" || (!hostPattern.MatchString(input.Host) && net.ParseIP(input.Host) == nil) || strings.Contains(input.Host, "..") {
		return input, &ValidationError{Message: "valid SMTP host is required"}
	}
	if input.Port < 1 || input.Port > 65535 {
		return input, &ValidationError{Message: "SMTP port must be between 1 and 65535"}
	}
	if input.TLSMode != "starttls" && input.TLSMode != "tls" {
		return input, &ValidationError{Message: "SMTP TLS mode must be starttls or tls"}
	}
	if len(input.Username) > 320 || len(input.Password) > 4096 {
		return input, &ValidationError{Message: "SMTP credentials are too long"}
	}
	if input.ClearPassword && input.Password != "" {
		return input, &ValidationError{Message: "SMTP password cannot be set and cleared in the same request"}
	}
	if input.Username == "" && input.Password != "" {
		return input, &ValidationError{Message: "SMTP username is required when a password is configured"}
	}
	from, err := mail.ParseAddress(input.FromAddress)
	if err != nil || from.Address != input.FromAddress {
		return input, &ValidationError{Message: "valid SMTP from address is required"}
	}
	seen := map[string]struct{}{}
	rawRecipients := input.Recipients
	input.Recipients = nil
	for _, value := range rawRecipients {
		address, err := mail.ParseAddress(strings.TrimSpace(value))
		if err != nil || address.Address != strings.TrimSpace(value) {
			return input, &ValidationError{Message: "all SMTP recipients must be valid email addresses"}
		}
		seen[strings.ToLower(address.Address)] = struct{}{}
	}
	for address := range seen {
		input.Recipients = append(input.Recipients, address)
	}
	sort.Strings(input.Recipients)
	if len(input.Recipients) < 1 || len(input.Recipients) > 20 {
		return input, &ValidationError{Message: "between 1 and 20 SMTP recipients are required"}
	}
	return input, nil
}

func settingView(record *ent.NotificationSetting) SettingView {
	updated := record.UpdatedAt
	return SettingView{Configured: true, Enabled: record.Enabled, Host: record.Host, Port: record.Port, TLSMode: string(record.TLSMode),
		Username: record.Username, PasswordConfigured: record.PasswordCiphertext != "", FromAddress: record.FromAddress,
		Recipients: record.Recipients, Status: string(record.Status), LastTestedAt: record.LastTestedAt, LastError: record.LastError, UpdatedAt: &updated}
}

func notificationView(record *ent.Notification) NotificationView {
	return NotificationView{ID: record.PublicID.String(), Kind: record.Kind, Severity: string(record.Severity), Subject: record.Subject,
		State: string(record.State), Attempts: record.Attempts, NextAttemptAt: record.NextAttemptAt,
		LastError: record.LastError, SentAt: record.SentAt, CreatedAt: record.CreatedAt}
}
