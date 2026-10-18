// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Redacted replaces secret values in recorded fixtures. Replay runs with
// Credentials.Secret set to this value, so URLs and headers that embed the
// key match the recording.
const Redacted = "REDACTED"

// Fixture is a recorded conversation with a provider, stored as JSON in the
// adapter's testdata directory.
type Fixture struct {
	// RecordedAt pins HTTPClient.Now during replay.
	RecordedAt   time.Time     `json:"recorded_at"`
	Interactions []Interaction `json:"interactions"`
}

// Interaction is one request and its response. Request headers are not
// stored: they carry credentials and don't take part in matching.
type Interaction struct {
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	RequestBody json.RawMessage `json:"request_body,omitempty"`
	Status      int             `json:"status"`
	// Header keeps only Content-Type, Retry-After and Link (paging).
	Header map[string]string `json:"header,omitempty"`
	// Body is the response when it is JSON; BodyText otherwise.
	Body     json.RawMessage `json:"body,omitempty"`
	BodyText string          `json:"body_text,omitempty"`
}

var keptHeaders = []string{"Content-Type", "Retry-After", "Link"}

// Recorder is an http.RoundTripper that forwards to Next and records every
// exchange, replacing each value in Secrets with Redacted. Review recorded
// response bodies for account ids or emails before committing them.
type Recorder struct {
	Next    http.RoundTripper
	Secrets []string

	mu      sync.Mutex
	fixture Fixture
}

// NewRecorder returns a recorder over next (http.DefaultTransport when nil).
func NewRecorder(next http.RoundTripper, secrets ...string) *Recorder {
	if next == nil {
		next = http.DefaultTransport
	}
	var nonEmpty []string
	for _, s := range secrets {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return &Recorder{Next: next, Secrets: nonEmpty, fixture: Fixture{RecordedAt: time.Now().UTC().Truncate(time.Second)}}
}

// RoundTrip implements http.RoundTripper.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
		_ = req.Body.Close()
		reqBody = b
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	resp, err := r.Next.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // transport errors pass through unchanged
	}
	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	in := Interaction{
		Method: req.Method,
		URL:    r.redact(req.URL.String()),
		Status: resp.StatusCode,
	}
	if len(reqBody) > 0 {
		in.RequestBody = json.RawMessage(r.redact(string(reqBody)))
	}
	for _, h := range keptHeaders {
		if v := resp.Header.Get(h); v != "" {
			if in.Header == nil {
				in.Header = map[string]string{}
			}
			in.Header[h] = v
		}
	}
	red := r.redact(string(respBody))
	if json.Valid([]byte(red)) {
		in.Body = json.RawMessage(red)
	} else {
		in.BodyText = red
	}

	r.mu.Lock()
	r.fixture.Interactions = append(r.fixture.Interactions, in)
	r.mu.Unlock()
	return resp, nil
}

func (r *Recorder) redact(s string) string {
	for _, secret := range r.Secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	return s
}

// Save writes the recorded fixture as indented JSON.
func (r *Recorder) Save(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := json.MarshalIndent(r.fixture, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding fixture: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing fixture: %w", err)
	}
	return nil
}

// Replayer is an http.RoundTripper that answers from a fixture. Requests
// must arrive in recorded order with the same method, URL and body.
type Replayer struct {
	mu      sync.Mutex
	fixture Fixture
	next    int
}

// LoadReplayer reads a fixture file.
func LoadReplayer(path string) (*Replayer, error) {
	b, err := os.ReadFile(path) //nolint:gosec // test fixture path chosen by the test
	if err != nil {
		return nil, fmt.Errorf("reading fixture: %w", err)
	}
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("decoding fixture %s: %w", path, err)
	}
	return &Replayer{fixture: f}, nil
}

// RecordedAt is when the fixture was recorded.
func (r *Replayer) RecordedAt() time.Time { return r.fixture.RecordedAt }

// Unused returns how many recorded interactions were never requested.
func (r *Replayer) Unused() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.fixture.Interactions) - r.next
}

// RoundTrip implements http.RoundTripper.
func (r *Replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
		_ = req.Body.Close()
		body = b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next >= len(r.fixture.Interactions) {
		return nil, fmt.Errorf("fixture: unexpected request %s %s (all %d interactions used)", req.Method, req.URL, len(r.fixture.Interactions))
	}
	in := r.fixture.Interactions[r.next]
	if in.Method != req.Method || in.URL != req.URL.String() {
		return nil, fmt.Errorf("fixture: request %d is %s %s, recorded %s %s", r.next, req.Method, req.URL, in.Method, in.URL)
	}
	if !jsonEqual(in.RequestBody, body) {
		return nil, fmt.Errorf("fixture: request %d body %s differs from recorded %s", r.next, body, in.RequestBody)
	}
	r.next++

	resp := &http.Response{
		StatusCode: in.Status,
		Status:     fmt.Sprintf("%d %s", in.Status, http.StatusText(in.Status)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Request:    req,
	}
	for k, v := range in.Header {
		resp.Header.Set(k, v)
	}
	b := []byte(in.BodyText)
	if len(in.Body) > 0 {
		b = in.Body
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	return resp, nil
}

func jsonEqual(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}
