// SPDX-License-Identifier: SUL-1.0

package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/version"
)

// action is one catalog link rendered as a line in the text body and a
// button in the HTML body.
type action struct {
	Label string
	URL   string
}

// emailView is what the templates render.
type emailView struct {
	Product string
	Title   string
	Body    string
	Actions []action
	OpenURL string
}

var textBody = texttemplate.Must(texttemplate.New("text").Parse(`{{.Body}}
{{range .Actions}}
{{.Label}}: {{.URL}}
{{end}}
Open in {{.Product}}: {{.OpenURL}}
`))

// htmlBody is a single inline-styled column: no external images, no
// tracking pixels, no remote CSS.
var htmlBody = htmltemplate.Must(htmltemplate.New("html").Parse(`<!doctype html>
<html>
<body style="margin:0;padding:24px;background:#f6f7f9;color:#111827;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
<tr><td align="center">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%;background:#ffffff;border:1px solid #e5e7eb;border-radius:8px;">
<tr><td style="padding:24px;font-size:15px;line-height:1.6;">
<h1 style="margin:0 0 16px;font-size:18px;line-height:1.3;">{{.Title}}</h1>
<p style="margin:0 0 16px;white-space:pre-line;">{{.Body}}</p>
{{range .Actions}}<p style="margin:0 0 8px;"><a href="{{.URL}}" style="display:inline-block;padding:8px 14px;background:#111827;color:#ffffff;text-decoration:none;border-radius:6px;">{{.Label}}</a></p>
{{end}}<p style="margin:16px 0 0;"><a href="{{.OpenURL}}" style="color:#2563eb;">Open in {{.Product}}</a></p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

// renderMessage turns one inbox alert into a message. The subject is the
// alert title; the body is the alert body, one action per catalog link and
// a link back to the inbox.
func renderMessage(a db.Alert, publicURL string) (Message, error) {
	view := emailView{
		Product: version.Name,
		Title:   a.Title,
		Body:    a.Body,
		OpenURL: strings.TrimRight(publicURL, "/") + "/alerts",
	}

	if len(a.Data) > 0 && string(a.Data) != "null" {
		var data struct {
			Links struct {
				ManageSubscription string `json:"manage_subscription"`
				Cancel             string `json:"cancel"`
				TopUp              string `json:"top_up"`
				Billing            string `json:"billing"`
			} `json:"links"`
		}
		if err := json.Unmarshal(a.Data, &data); err != nil {
			return Message{}, fmt.Errorf("decoding alert links: %w", err)
		}
		links := []struct {
			label, url string
		}{
			{"Manage subscription", data.Links.ManageSubscription},
			{"Cancel", data.Links.Cancel},
			{"Top up", data.Links.TopUp},
			{"Billing", data.Links.Billing},
		}
		for _, l := range links {
			if l.url != "" {
				view.Actions = append(view.Actions, action{Label: l.label, URL: l.url})
			}
		}
	}

	var text bytes.Buffer
	if err := textBody.Execute(&text, view); err != nil {
		return Message{}, fmt.Errorf("rendering text email: %w", err)
	}
	var html bytes.Buffer
	if err := htmlBody.Execute(&html, view); err != nil {
		return Message{}, fmt.Errorf("rendering HTML email: %w", err)
	}
	return Message{Subject: a.Title, Text: text.String(), HTML: html.String()}, nil
}
