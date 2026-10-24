// SPDX-License-Identifier: SUL-1.0

package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/version"
)

// channelClient is the HTTP client every sender uses. The timeout bounds one
// delivery attempt so River retries instead of hanging.
var channelClient = &http.Client{Timeout: 10 * time.Second}

// maxErrorBody is how much of a provider response an error carries.
const maxErrorBody = 200

// maxDiscordContent is Discord's message limit, in characters.
const maxDiscordContent = 2000

// channelMessage is one alert rendered for a channel.
type channelMessage struct {
	ID        string
	Kind      string
	Severity  string
	Title     string
	Body      string
	AccountID *string
	CreatedAt time.Time
	URL       string
	Links     map[string]string
}

// sendError is a non-2xx answer. It carries the status and a short excerpt of
// the body, never the request URL or the channel secret.
type sendError struct {
	status int
	body   string
}

func (e *sendError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("provider answered %d", e.status)
	}
	return fmt.Sprintf("provider answered %d: %s", e.status, e.body)
}

// sendChannel delivers m through one channel. secret is the revealed webhook
// URL or token; it is empty for an ntfy channel without a token.
func sendChannel(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	switch ch.Kind {
	case "webhook":
		return sendWebhook(ctx, ch, secret, m)
	case "ntfy":
		return sendNtfy(ctx, ch, secret, m)
	case "telegram":
		return sendTelegram(ctx, ch, secret, m)
	case "discord":
		return sendDiscord(ctx, ch, secret, m)
	case "slack":
		return sendSlack(ctx, ch, secret, m)
	default:
		return fmt.Errorf("sending %s message: unknown channel kind", ch.Kind)
	}
}

// messageForAlert renders one inbox alert the way the email does: the alert
// title, its body and a link back to the hub inbox.
func messageForAlert(a db.Alert, publicURL string) channelMessage {
	return channelMessage{
		ID:        a.ID,
		Kind:      a.Kind,
		Severity:  a.Severity,
		Title:     a.Title,
		Body:      a.Body,
		AccountID: a.AccountID,
		CreatedAt: a.CreatedAt,
		URL:       alertsURL(publicURL),
		Links:     alertLinks(a.Data),
	}
}

// testMessage is the fixed message the "send test" operation delivers.
func testMessage(publicURL string) channelMessage {
	return channelMessage{
		Kind:      "test",
		Severity:  "info",
		Title:     version.Name + " test message",
		Body:      "This is a test message from " + version.Name + ".",
		CreatedAt: time.Now().UTC(),
		URL:       alertsURL(publicURL),
		Links:     map[string]string{},
	}
}

func alertsURL(publicURL string) string {
	return strings.TrimRight(publicURL, "/") + "/alerts"
}

// alertLinks decodes the catalog links the alert carries in data.links.
func alertLinks(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	var data struct {
		Links struct {
			ManageSubscription string `json:"manage_subscription"`
			Cancel             string `json:"cancel"`
			TopUp              string `json:"top_up"`
			Billing            string `json:"billing"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return out
	}
	for key, value := range map[string]string{
		"manage_subscription": data.Links.ManageSubscription,
		"cancel":              data.Links.Cancel,
		"top_up":              data.Links.TopUp,
		"billing":             data.Links.Billing,
	} {
		if value != "" {
			out[key] = value
		}
	}
	return out
}

// sendWebhook posts the alert as JSON to the channel's URL.
func sendWebhook(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	if secret == "" {
		return errors.New("sending webhook message: channel has no URL")
	}
	links := m.Links
	if links == nil {
		links = map[string]string{}
	}
	payload := struct {
		ID        string            `json:"id"`
		Kind      string            `json:"kind"`
		Severity  string            `json:"severity"`
		Title     string            `json:"title"`
		Body      string            `json:"body"`
		AccountID *string           `json:"account_id"`
		CreatedAt time.Time         `json:"created_at"`
		URL       string            `json:"url"`
		Links     map[string]string `json:"links"`
	}{
		ID:        m.ID,
		Kind:      m.Kind,
		Severity:  m.Severity,
		Title:     m.Title,
		Body:      m.Body,
		AccountID: m.AccountID,
		CreatedAt: m.CreatedAt,
		URL:       m.URL,
		Links:     links,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sending webhook message: %w", err)
	}
	return post(ctx, "webhook", secret, "application/json", body, nil)
}

// sendNtfy posts the body and the hub link to an ntfy topic.
func sendNtfy(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	config, err := decodeConfig(ch.Config)
	if err != nil {
		return err
	}
	server := strings.TrimSpace(config["server"])
	if server == "" {
		server = defaultNtfyServer
	}
	topic := strings.TrimSpace(config["topic"])
	if topic == "" {
		return errors.New("sending ntfy message: channel has no topic")
	}
	endpoint := strings.TrimRight(server, "/") + "/" + url.PathEscape(topic)

	text := strings.TrimRight(m.Body, "\n")
	if m.URL != "" {
		text += "\n\n" + m.URL
	}
	priority, tag := ntfySeverity(m.Severity)
	headers := map[string]string{
		"Title":    m.Title,
		"Priority": priority,
		"Tags":     tag,
		"Click":    m.URL,
	}
	if secret != "" {
		headers["Authorization"] = "Bearer " + secret
	}
	return post(ctx, "ntfy", endpoint, "text/plain; charset=utf-8", []byte(text), headers)
}

func ntfySeverity(severity string) (priority, tag string) {
	switch severity {
	case "critical":
		return "5", "rotating_light"
	case "info":
		return "3", "information_source"
	default:
		return "4", "warning"
	}
}

// sendTelegram posts a plain-text message to the bot's chat. Plain text
// avoids MarkdownV2 escaping mistakes; disable_web_page_preview keeps the
// link compact.
func sendTelegram(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	if secret == "" {
		return errors.New("sending telegram message: channel has no bot token")
	}
	config, err := decodeConfig(ch.Config)
	if err != nil {
		return err
	}
	chatID := strings.TrimSpace(config["chat_id"])
	if chatID == "" {
		return errors.New("sending telegram message: channel has no chat_id")
	}
	text := joinNonEmpty("\n\n", m.Title, m.Body, m.URL)
	payload := struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}{
		ChatID:                chatID,
		Text:                  text,
		DisableWebPagePreview: true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	endpoint := "https://api.telegram.org/bot" + secret + "/sendMessage"
	return post(ctx, "telegram", endpoint, "application/json", body, nil)
}

// sendDiscord posts one message to an incoming webhook.
func sendDiscord(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	if secret == "" {
		return errors.New("sending discord message: channel has no webhook URL")
	}
	prefix := "**" + m.Title + "**\n"
	suffix := "\n<" + m.URL + ">"
	body := m.Body
	if room := maxDiscordContent - len([]rune(prefix)) - len([]rune(suffix)); room < len([]rune(body)) {
		if room < 0 {
			room = 0
		}
		body = truncateRunes(body, room)
	}
	payload := struct {
		Content string `json:"content"`
	}{Content: prefix + body + suffix}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sending discord message: %w", err)
	}
	return post(ctx, "discord", secret, "application/json", encoded, nil)
}

// sendSlack posts one message to an incoming webhook.
func sendSlack(ctx context.Context, ch db.NotificationChannel, secret string, m channelMessage) error {
	if secret == "" {
		return errors.New("sending slack message: channel has no webhook URL")
	}
	text := joinNonEmpty("\n", "*"+m.Title+"*", m.Body, "<"+m.URL+">")
	payload := struct {
		Text string `json:"text"`
	}{Text: text}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sending slack message: %w", err)
	}
	return post(ctx, "slack", secret, "application/json", encoded, nil)
}

// post sends one request and turns a non-2xx answer into a sendError. The
// returned error never contains the endpoint, which carries the token.
func post(ctx context.Context, kind, endpoint, contentType string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending %s message: invalid request", kind)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := channelClient.Do(req)
	if err != nil {
		// url.Error repeats the full URL (token included); keep only its
		// cause, which names at most the host ("no such host", timeouts).
		reason := "request failed"
		var ue *url.Error
		if errors.As(err, &ue) && ue.Err != nil {
			reason = ue.Err.Error()
		}
		return fmt.Errorf("sending %s message: %s", kind, reason)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &sendError{status: resp.StatusCode, body: strings.TrimSpace(string(excerpt))}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, sep)
}

// truncateRunes keeps the first n characters, never splitting a rune.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
