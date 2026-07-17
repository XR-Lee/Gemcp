package notification

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	entnotification "github.com/XR-Lee/Gemcp/ent/notification"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type notificationFixture struct {
	client  *ent.Client
	box     *secrets.Box
	service *Service
	tenant  *ent.Tenant
	now     time.Time
}

func newNotificationFixture(t *testing.T) *notificationFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	tenant, _ := client.Tenant.Create().SetName("tenant").Save(context.Background())
	fixture := &notificationFixture{client: client, box: box, tenant: tenant, now: time.Now().UTC().Truncate(time.Second)}
	fixture.service = NewService(client, box)
	fixture.service.now = func() time.Time { return fixture.now }
	return fixture
}

func validSMTPInput() ConfigureInput {
	return ConfigureInput{
		Enabled: true, Host: "smtp.example.com", Port: 587, TLSMode: "starttls",
		Username: "mailer@example.com", Password: "smtp-password-secret",
		FromAddress: "mailer@example.com", Recipients: []string{"owner@example.com"},
	}
}

func TestConfigureEncryptsSMTPPasswordAndNeverReturnsIt(t *testing.T) {
	f := newNotificationFixture(t)
	view, err := f.service.Configure(context.Background(), f.tenant.ID, "owner-1", validSMTPInput())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Configured || !view.PasswordConfigured || len(view.Recipients) != 1 {
		t.Fatalf("view = %+v", view)
	}
	record, _ := f.client.NotificationSetting.Query().Only(context.Background())
	if record.PasswordCiphertext == "smtp-password-secret" || strings.Contains(record.PasswordCiphertext, "smtp-password-secret") {
		t.Fatal("SMTP password was stored in plaintext")
	}
	plaintext, err := f.box.Decrypt(record.PasswordCiphertext, smtpPasswordAADPrefix+f.tenant.PublicID.String())
	if err != nil || string(plaintext) != "smtp-password-secret" {
		t.Fatalf("decrypted password=%q err=%v", plaintext, err)
	}
	update := validSMTPInput()
	update.Password = ""
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner-1", update); err != nil {
		t.Fatal(err)
	}
	updated, _ := f.client.NotificationSetting.Get(context.Background(), record.ID)
	if updated.PasswordCiphertext != record.PasswordCiphertext {
		t.Fatal("blank password unexpectedly removed the stored credential")
	}
	update.Username = ""
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner-1", update); err == nil {
		t.Fatal("removing the username preserved an unusable stored password")
	}
	update.ClearPassword = true
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner-1", update); err != nil {
		t.Fatal(err)
	}
	updated, _ = f.client.NotificationSetting.Get(context.Background(), record.ID)
	if updated.PasswordCiphertext != "" {
		t.Fatal("clear_password did not remove the encrypted credential")
	}
}

func TestConfigureRejectsUnsafeOrIncompleteSMTPSettings(t *testing.T) {
	f := newNotificationFixture(t)
	input := validSMTPInput()
	input.Host = "https://smtp.example.com"
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner", input); err == nil {
		t.Fatal("Configure accepted SMTP URL as a host")
	}
	input = validSMTPInput()
	input.Recipients = nil
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner", input); err == nil {
		t.Fatal("Configure accepted no recipients")
	}
}

type fakeMailer struct {
	messages []SMTPMessage
	err      error
}

func (m *fakeMailer) Send(_ context.Context, message SMTPMessage) error {
	m.messages = append(m.messages, message)
	return m.err
}

func TestWorkerDeliversDurableOutboxAndMarksTested(t *testing.T) {
	f := newNotificationFixture(t)
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner", validSMTPInput()); err != nil {
		t.Fatal(err)
	}
	record, err := f.service.EnqueueTest(context.Background(), f.tenant.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	mailer := &fakeMailer{}
	worker := NewWorker(f.client, f.box, mailer, time.Second)
	worker.now = func() time.Time { return f.now }
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(mailer.messages) != 1 || mailer.messages[0].Password != "smtp-password-secret" {
		t.Fatalf("messages = %+v", mailer.messages)
	}
	delivered, _ := f.client.Notification.Query().Where(entnotification.PublicIDEQ(mustUUID(t, record.ID))).Only(context.Background())
	if delivered.State != entnotification.StateSent || delivered.SentAt == nil {
		t.Fatalf("notification = %+v", delivered)
	}
	setting, _ := f.client.NotificationSetting.Query().Only(context.Background())
	if setting.LastTestedAt == nil || setting.Status != "ready" {
		t.Fatalf("setting = %+v", setting)
	}
}

func TestWorkerRetriesThenFailsWithoutLeakingCredential(t *testing.T) {
	f := newNotificationFixture(t)
	if _, err := f.service.Configure(context.Background(), f.tenant.ID, "owner", validSMTPInput()); err != nil {
		t.Fatal(err)
	}
	record, _ := f.client.Notification.Create().SetTenantID(f.tenant.ID).SetDedupKey("failure-1").SetKind("test").
		SetSeverity("critical").SetSubject("Failure").SetBody("body").SetAttempts(maxDeliveryAttempts - 1).SetNextAttemptAt(f.now).Save(context.Background())
	mailer := &fakeMailer{err: errors.New("authentication failed: smtp-password-secret")}
	worker := NewWorker(f.client, f.box, mailer, time.Second)
	worker.now = func() time.Time { return f.now }
	if err := worker.Tick(context.Background()); err == nil {
		t.Fatal("Tick accepted delivery failure")
	}
	record, _ = f.client.Notification.Get(context.Background(), record.ID)
	if record.State != entnotification.StateFailed || record.LastError == nil || strings.Contains(*record.LastError, "smtp-password-secret") {
		t.Fatalf("notification = %+v", record)
	}
}

func TestWorkerDoesNotExceedMaximumAfterStaleFinalClaim(t *testing.T) {
	f := newNotificationFixture(t)
	record, err := f.client.Notification.Create().SetTenantID(f.tenant.ID).SetDedupKey("stale-final").SetKind("test").
		SetSeverity("critical").SetSubject("Failure").SetBody("body").SetState(entnotification.StateSending).
		SetAttempts(maxDeliveryAttempts).SetUpdatedAt(f.now.Add(-6 * time.Minute)).SetNextAttemptAt(f.now).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mailer := &fakeMailer{}
	worker := NewWorker(f.client, f.box, mailer, time.Second)
	worker.now = func() time.Time { return f.now }
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ = f.client.Notification.Get(context.Background(), record.ID)
	if record.State != entnotification.StateFailed || record.Attempts != maxDeliveryAttempts || len(mailer.messages) != 0 {
		t.Fatalf("notification=%+v messages=%d", record, len(mailer.messages))
	}
}

func TestEnqueueDeduplicatesAndUnconfiguredDeliveryStaysPending(t *testing.T) {
	f := newNotificationFixture(t)
	input := EnqueueInput{TenantID: f.tenant.ID, DedupKey: "timeout:experiment-1", Kind: "timeout", Severity: "critical", Subject: "Timeout", Body: "body"}
	if err := Enqueue(context.Background(), f.client.Notification, input); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(context.Background(), f.client.Notification, input); err != nil {
		t.Fatal(err)
	}
	if count, _ := f.client.Notification.Query().Count(context.Background()); count != 1 {
		t.Fatalf("notification count = %d", count)
	}
	worker := NewWorker(f.client, f.box, &fakeMailer{}, time.Second)
	worker.now = func() time.Time { return f.now }
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ := f.client.Notification.Query().Only(context.Background())
	if record.State != entnotification.StatePending || record.Attempts != 0 || !record.NextAttemptAt.After(f.now) {
		t.Fatalf("notification = %+v", record)
	}
}

func TestSMTPMessageSanitizesSubjectHeaders(t *testing.T) {
	var output bytes.Buffer
	message := SMTPMessage{FromAddress: "from@example.com", Recipients: []string{"to@example.com"}, Subject: "hello\r\nBcc: hidden@example.com", Body: "body"}
	if err := writeMessage(&output, message); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\r\nBcc:") || !strings.Contains(output.String(), "Subject: hello  Bcc:") {
		t.Fatalf("message = %q", output.String())
	}
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
