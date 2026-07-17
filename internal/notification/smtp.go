package notification

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type SMTPMessage struct {
	Host        string
	Port        int
	TLSMode     string
	Username    string
	Password    string
	FromAddress string
	Recipients  []string
	Subject     string
	Body        string
}

type Mailer interface {
	Send(context.Context, SMTPMessage) error
}

type SMTPMailer struct {
	Timeout time.Duration
}

func (m SMTPMailer) Send(ctx context.Context, message SMTPMessage) error {
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	address := net.JoinHostPort(message.Host, fmt.Sprintf("%d", message.Port))
	dialer := &net.Dialer{Timeout: timeout}
	var connection net.Conn
	var err error
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: message.Host}
	if message.TLSMode == "tls" {
		connection, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(connection, message.Host)
	if err != nil {
		return fmt.Errorf("initialize SMTP client: %w", err)
	}
	defer client.Close()
	if message.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if message.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", message.Username, message.Password, message.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	if err := client.Mail(message.FromAddress); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	for _, recipient := range message.Recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set SMTP recipient: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	if err := writeMessage(writer, message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP session: %w", err)
	}
	return nil
}

func writeMessage(writer io.Writer, message SMTPMessage) error {
	subject := strings.ReplaceAll(strings.ReplaceAll(message.Subject, "\r", " "), "\n", " ")
	body := strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\r", "\n")
	buffer := bufio.NewWriter(writer)
	headers := []string{
		"From: " + message.FromAddress,
		"To: " + strings.Join(message.Recipients, ", "),
		"Subject: " + subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
	}
	if _, err := buffer.WriteString(strings.Join(headers, "\r\n")); err != nil {
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("flush SMTP message: %w", err)
	}
	return nil
}
