package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	entnotification "github.com/XR-Lee/Gemcp/ent/notification"
	"github.com/XR-Lee/Gemcp/ent/notificationsetting"
	"github.com/XR-Lee/Gemcp/ent/serviceheartbeat"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/servicehealth"
)

const maxDeliveryAttempts = 5

type Worker struct {
	client       *ent.Client
	box          *secrets.Box
	mailer       Mailer
	pollInterval time.Duration
	instanceID   string
	now          func() time.Time
}

type WorkerOption func(*Worker)

func WithWorkerInstanceID(value string) WorkerOption {
	return func(worker *Worker) {
		if strings.TrimSpace(value) != "" {
			worker.instanceID = strings.TrimSpace(value)
		}
	}
}

func NewWorker(client *ent.Client, box *secrets.Box, mailer Mailer, pollInterval time.Duration, options ...WorkerOption) *Worker {
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}
	if mailer == nil {
		mailer = SMTPMailer{}
	}
	worker := &Worker{client: client, box: box, mailer: mailer, pollInterval: pollInterval, instanceID: "notification-worker", now: time.Now}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("notification worker tick failed", "error", err)
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("notification worker tick failed", "error", err)
			}
		}
	}
}

func (w *Worker) Tick(ctx context.Context) error {
	now := w.now().UTC()
	heartbeatErr := servicehealth.Beat(ctx, w.client, serviceheartbeat.RoleNotification, w.instanceID, map[string]any{
		"poll_interval_seconds": w.pollInterval.Seconds(),
	})
	staleBefore := now.Add(-5 * time.Minute)
	_, _ = w.client.Notification.Update().Where(
		entnotification.StateEQ(entnotification.StateSending), entnotification.UpdatedAtLT(staleBefore), entnotification.AttemptsGTE(maxDeliveryAttempts),
	).SetState(entnotification.StateFailed).SetLastError("notification worker was interrupted during the final delivery attempt").Save(ctx)
	_, _ = w.client.Notification.Update().Where(
		entnotification.StateEQ(entnotification.StateSending), entnotification.UpdatedAtLT(staleBefore), entnotification.AttemptsLT(maxDeliveryAttempts),
	).SetState(entnotification.StatePending).SetNextAttemptAt(now).Save(ctx)
	_, _ = w.client.Notification.Update().Where(
		entnotification.StateEQ(entnotification.StatePending), entnotification.AttemptsGTE(maxDeliveryAttempts),
	).SetState(entnotification.StateFailed).SetLastError("maximum delivery attempts exhausted").Save(ctx)
	records, err := w.client.Notification.Query().Where(
		entnotification.StateEQ(entnotification.StatePending), entnotification.AttemptsLT(maxDeliveryAttempts), entnotification.NextAttemptAtLTE(now),
	).Order(ent.Asc(entnotification.FieldCreatedAt)).Limit(20).All(ctx)
	if err != nil {
		return err
	}
	var tickErrors []error
	for _, record := range records {
		claimed, err := w.client.Notification.Update().Where(
			entnotification.IDEQ(record.ID), entnotification.StateEQ(entnotification.StatePending), entnotification.AttemptsLT(maxDeliveryAttempts), entnotification.NextAttemptAtLTE(now),
		).SetState(entnotification.StateSending).AddAttempts(1).Save(ctx)
		if err != nil {
			tickErrors = append(tickErrors, err)
			continue
		}
		if claimed != 1 {
			continue
		}
		record, err = w.client.Notification.Get(ctx, record.ID)
		if err != nil {
			tickErrors = append(tickErrors, err)
			continue
		}
		if err := w.deliver(ctx, record, now); err != nil {
			tickErrors = append(tickErrors, err)
		}
	}
	return errors.Join(append([]error{heartbeatErr}, tickErrors...)...)
}

func (w *Worker) deliver(ctx context.Context, record *ent.Notification, now time.Time) error {
	setting, err := w.client.NotificationSetting.Query().Where(
		notificationsetting.TenantIDEQ(record.TenantID), notificationsetting.EnabledEQ(true),
	).Only(ctx)
	if ent.IsNotFound(err) {
		_, updateErr := record.Update().SetState(entnotification.StatePending).SetNextAttemptAt(now.Add(time.Hour)).AddAttempts(-1).
			SetLastError("SMTP notifications are not configured or enabled").Save(ctx)
		return updateErr
	}
	if err != nil {
		return w.deliveryFailed(record, nil, now, err)
	}
	tenantRecord, err := w.client.Tenant.Get(ctx, record.TenantID)
	if err != nil {
		return w.deliveryFailed(record, setting, now, err)
	}
	password := ""
	if setting.PasswordCiphertext != "" {
		plaintext, err := w.box.Decrypt(setting.PasswordCiphertext, smtpPasswordAADPrefix+tenantRecord.PublicID.String())
		if err != nil {
			return w.deliveryFailed(record, setting, now, fmt.Errorf("decrypt SMTP password: %w", err))
		}
		password = string(plaintext)
		for index := range plaintext {
			plaintext[index] = 0
		}
	}
	message := SMTPMessage{
		Host: setting.Host, Port: setting.Port, TLSMode: string(setting.TLSMode), Username: setting.Username, Password: password,
		FromAddress: setting.FromAddress, Recipients: setting.Recipients, Subject: record.Subject, Body: record.Body,
	}
	sendCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	err = w.mailer.Send(sendCtx, message)
	cancel()
	if err != nil {
		err = redactSecret(err, password)
		message.Password = ""
		password = ""
		return w.deliveryFailed(record, setting, now, err)
	}
	message.Password = ""
	password = ""
	if _, err := record.Update().SetState(entnotification.StateSent).SetSentAt(now).ClearLastError().Save(ctx); err != nil {
		return err
	}
	settingUpdate := setting.Update().SetStatus(notificationsetting.StatusReady).ClearLastError()
	if record.Kind == "smtp_test" {
		settingUpdate.SetLastTestedAt(now)
	}
	_, err = settingUpdate.Save(ctx)
	return err
}

func (w *Worker) deliveryFailed(record *ent.Notification, setting *ent.NotificationSetting, now time.Time, cause error) error {
	message := boundedError(cause)
	update := record.Update().SetLastError(message)
	if record.Attempts >= maxDeliveryAttempts {
		update.SetState(entnotification.StateFailed)
	} else {
		backoff := time.Duration(1<<min(record.Attempts, 6)) * time.Minute
		update.SetState(entnotification.StatePending).SetNextAttemptAt(now.Add(backoff))
	}
	updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, updateErr := update.Save(updateCtx)
	if setting != nil {
		_, _ = setting.Update().SetStatus(notificationsetting.StatusError).SetLastError(message).Save(updateCtx)
	}
	return errors.Join(cause, updateErr)
}

func redactSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[REDACTED]"))
}

func boundedError(err error) string {
	value := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512]) + "..."
	}
	return value
}
