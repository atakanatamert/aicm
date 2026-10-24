// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/atakanatamert/aicm/internal/notify"
	"github.com/atakanatamert/aicm/internal/settings"
	"github.com/atakanatamert/aicm/internal/version"
)

// sourceRepo is the project's source repository, linked from the UI footer
// next to the running version.
const sourceRepo = "https://github.com/atakanatamert/aicm"

// mapSettingsError maps the settings service's sentinel errors to HTTP.
func mapSettingsError(err error) error {
	switch {
	case errors.Is(err, settings.ErrForbidden):
		return ErrForbidden
	case errors.Is(err, settings.ErrNotFound):
		return NotFound("settings not found")
	case errors.Is(err, settings.ErrInvalid):
		return BadRequest(err.Error())
	}
	return err
}

// sourceURL is the repository, pinned to the tag when the build version is
// one.
func sourceURL() string {
	if strings.HasPrefix(version.Version, "v") {
		return sourceRepo + "/tree/" + version.Version
	}
	return sourceRepo
}

// toSettingsResponse maps the service type. Non-admins see the SMTP host,
// port, TLS and from; the username and password metadata stay admin-only.
func toSettingsResponse(v settings.Settings, admin bool) Settings {
	out := Settings{
		DisplayCurrency: v.DisplayCurrency,
		FxSource:        SettingsFxSource(v.FXSource),
		Version:         version.Version,
		SourceUrl:       sourceURL(),
		Smtp: SmtpSettings{
			Host: v.SMTP.Host,
			Port: v.SMTP.Port,
			Tls:  SmtpSettingsTls(v.SMTP.TLS),
			From: v.SMTP.From,
		},
	}
	if !admin {
		return out
	}
	out.Smtp.Username = v.SMTP.Username
	out.Smtp.Password = SecretInfo{Set: v.SMTP.PasswordSet}
	if v.SMTP.PasswordSet {
		if v.SMTP.Password.Last4 != "" {
			last4 := v.SMTP.Password.Last4
			out.Smtp.Password.Last4 = &last4
		}
		updated := v.SMTP.Password.UpdatedAt
		out.Smtp.Password.UpdatedAt = &updated
	}
	return out
}

// GetSettings returns the org's settings.
func (s *Server) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.Settings.Get(ctx, p.OrgID)
	if err != nil {
		return nil, mapSettingsError(err)
	}
	return GetSettings200JSONResponse(toSettingsResponse(v, p.IsAdmin())), nil
}

// UpdateSettings patches the org's settings. The password is write-only.
func (s *Server) UpdateSettings(ctx context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}

	var u settings.Update
	if req.Body.DisplayCurrency != nil {
		currency := *req.Body.DisplayCurrency
		u.DisplayCurrency = &currency
	}
	if req.Body.FxSource != nil {
		source := string(*req.Body.FxSource)
		u.FXSource = &source
	}
	if smtp := req.Body.Smtp; smtp != nil {
		su := &struct {
			Host, Username, From, TLS *string
			Port                      *int
			Password                  *string
		}{
			Host:     smtp.Host,
			Username: smtp.Username,
			From:     smtp.From,
			Port:     smtp.Port,
			Password: smtp.Password,
		}
		if smtp.Tls != nil {
			tls := string(*smtp.Tls)
			su.TLS = &tls
		}
		u.SMTP = su
	}

	v, err := s.Settings.Update(ctx, p, u)
	if err != nil {
		return nil, mapSettingsError(err)
	}
	return UpdateSettings200JSONResponse(toSettingsResponse(v, true)), nil
}

// SendTestEmail sends one message through the configured SMTP server.
func (s *Server) SendTestEmail(ctx context.Context, req SendTestEmailRequestObject) (SendTestEmailResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	to := strings.TrimSpace(req.Body.To)
	if to == "" {
		return nil, BadRequest("to is required")
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return nil, BadRequest("to must be a valid email address")
	}

	cfg, configured, err := s.Settings.SMTP(ctx, p.OrgID)
	if err != nil {
		return nil, mapSettingsError(err)
	}
	if !configured {
		return nil, &APIError{
			Status:  http.StatusBadGateway,
			Code:    "smtp_failed",
			Message: "SMTP is not configured",
		}
	}

	subject := version.Name + " test email"
	text := fmt.Sprintf("This is a test email from %s. If you received it, alert delivery is configured correctly.\n", version.Name)
	html := fmt.Sprintf(`<!doctype html>
<html>
<body style="margin:0;padding:24px;background:#f6f7f9;color:#111827;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%%;background:#ffffff;border:1px solid #e5e7eb;border-radius:8px;">
<tr><td style="padding:24px;font-size:15px;line-height:1.6;">
<p style="margin:0;">This is a test email from %s. If you received it, alert delivery is configured correctly.</p>
</td></tr>
</table>
</body>
</html>
`, version.Name)

	if err := notify.NewSMTP(cfg).Send(ctx, notify.Message{
		To:      []string{to},
		Subject: subject,
		Text:    text,
		HTML:    html,
	}); err != nil {
		return nil, &APIError{
			Status:  http.StatusBadGateway,
			Code:    "smtp_failed",
			Message: err.Error(),
		}
	}
	return SendTestEmail204Response{}, nil
}
