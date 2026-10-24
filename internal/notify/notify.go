// SPDX-License-Identifier: SUL-1.0

// Package notify delivers alert emails. SMTP is the only Notifier in v1;
// the interface keeps Slack, Telegram or ntfy out of the alert logic.
package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wneessen/go-mail"
)

// Message is one email. To holds the recipients; with more than one the
// sender puts the rest in Bcc.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Notifier delivers a message. SMTP is the only implementation in v1.
type Notifier interface {
	Send(ctx context.Context, m Message) error
}

// SMTPConfig is one org's SMTP server.
type SMTPConfig struct {
	Host, Username, Password, From string
	Port                           int
	TLS                            string // none | starttls | tls
}

// NewSMTP returns a Notifier that connects to c for each Send. The client
// is built per call so a settings change applies to the next email.
func NewSMTP(c SMTPConfig) Notifier {
	return &smtp{cfg: c}
}

type smtp struct {
	cfg SMTPConfig
}

// Send delivers m over SMTP. It never logs; errors carry the server's
// response but never the configured password.
func (s *smtp) Send(ctx context.Context, m Message) error {
	if len(m.To) == 0 {
		return errors.New("sending mail: no recipients")
	}

	opts := []mail.Option{mail.WithTimeout(30 * time.Second)}
	if s.cfg.Port > 0 {
		opts = append(opts, mail.WithPort(s.cfg.Port))
	}
	switch s.cfg.TLS {
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case "tls":
		opts = append(opts, mail.WithSSL())
	default: // starttls, also the zero value
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	if s.cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.cfg.Username),
			mail.WithPassword(s.cfg.Password),
		)
	}

	client, err := mail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("creating SMTP client: %w", err)
	}

	msg := mail.NewMsg()
	if err := msg.From(s.cfg.From); err != nil {
		return fmt.Errorf("setting From address: %w", err)
	}
	if len(m.To) == 1 {
		if err := msg.To(m.To[0]); err != nil {
			return fmt.Errorf("setting recipient: %w", err)
		}
	} else {
		if err := msg.To(m.To[0]); err != nil {
			return fmt.Errorf("setting recipient: %w", err)
		}
		if err := msg.Bcc(m.To[1:]...); err != nil {
			return fmt.Errorf("setting recipients: %w", err)
		}
	}
	msg.Subject(m.Subject)
	msg.SetBodyString(mail.TypeTextPlain, m.Text)
	if m.HTML != "" {
		msg.AddAlternativeString(mail.TypeTextHTML, m.HTML)
	}

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("sending mail: %w", s.scrub(err))
	}
	return nil
}

// scrub removes the configured password from an error. go-mail does not
// include it today, but the notifier must never leak it through logs or an
// API response.
func (s *smtp) scrub(err error) error {
	if s.cfg.Password == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), s.cfg.Password, "[redacted]")
	return errors.New(message)
}
