// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/atakanatamert/aicm/internal/version"
)

// HTTPClient is the only way adapters talk to providers. It adds a timeout,
// retries throttled and failed requests with backoff, honours Retry-After,
// maps auth failures to ErrInvalidCredentials and never logs headers or
// query strings (they can hold keys).
type HTTPClient struct {
	hc  *http.Client
	log *slog.Logger
	// Now is the clock adapters use for CapturedAt. Fixture replay pins it.
	Now func() time.Time
	// MaxRetries is the number of retries after the first attempt.
	MaxRetries int
	// MaxRetryWait caps one wait. A longer Retry-After returns RateLimitError
	// at once so the sync engine can reschedule instead of blocking a worker.
	MaxRetryWait time.Duration
	// Sleep waits between retries. Fixture replay makes it instant.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewHTTPClient returns a client over rt (http.DefaultTransport when nil).
func NewHTTPClient(log *slog.Logger, rt http.RoundTripper) *HTTPClient {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &HTTPClient{
		hc:           &http.Client{Transport: rt, Timeout: 30 * time.Second},
		log:          log,
		Now:          time.Now,
		MaxRetries:   3,
		MaxRetryWait: time.Minute,
		Sleep:        sleepCtx,
	}
}

// HTTPError is a non-2xx response. It unwraps to ErrInvalidCredentials for
// 401 and 403.
type HTTPError struct {
	Method string
	// URL is scheme, host and path only; the query is dropped.
	URL    string
	Status int
	// Body is the start of the response body, for diagnosis.
	Body string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.Status, e.Body)
}

// Unwrap maps auth failures to ErrInvalidCredentials.
func (e *HTTPError) Unwrap() error {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return ErrInvalidCredentials
	}
	return nil
}

// GetJSON sends a GET and decodes a 2xx JSON body into out.
func (c *HTTPClient) GetJSON(ctx context.Context, url string, header http.Header, out any) error {
	return c.SendJSON(ctx, http.MethodGet, url, header, nil, out)
}

// SendJSON sends in (if not nil) as a JSON body and decodes a 2xx JSON body
// into out (if not nil). Requests are retried, so only use it for calls that
// are safe to repeat; provider reads always are.
func (c *HTTPClient) SendJSON(ctx context.Context, method, url string, header http.Header, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if body == nil {
		req.Body, req.GetBody, req.ContentLength = http.NoBody, nil, 0
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s %s response: %w", method, redactURL(req), err)
	}
	return nil
}

// Do sends req with retries and returns a 2xx response, or an error. The
// caller closes the body. A request with a body must have GetBody set
// (http.NewRequest does this for bytes and strings readers).
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	}
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("rewinding request body: %w", err)
			}
			req.Body = b
		}
		start := time.Now()
		resp, err := c.hc.Do(req)
		if err != nil {
			if ctx.Err() != nil || attempt >= c.MaxRetries {
				return nil, fmt.Errorf("%s %s: %w", req.Method, redactURL(req), err)
			}
			c.log.DebugContext(ctx, "provider request failed, retrying", "method", req.Method, "url", redactURL(req), "attempt", attempt+1, "err", err)
			if err := c.Sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		c.log.DebugContext(ctx, "provider request", "method", req.Method, "url", redactURL(req), "status", resp.StatusCode, "duration", time.Since(start))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout
		wait, hasRetryAfter := retryAfter(resp.Header.Get("Retry-After"), c.Now())
		if !hasRetryAfter {
			wait = backoff(attempt)
		}
		if !retryable || attempt >= c.MaxRetries || wait > c.MaxRetryWait {
			if resp.StatusCode == http.StatusTooManyRequests {
				drain(resp)
				return nil, &RateLimitError{RetryAfter: wait}
			}
			return nil, newHTTPError(req, resp)
		}
		drain(resp)
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func newHTTPError(req *http.Request, resp *http.Response) *HTTPError {
	defer drain(resp)
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &HTTPError{Method: req.Method, URL: redactURL(req), Status: resp.StatusCode, Body: string(b)}
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// redactURL returns scheme://host/path without the query.
func redactURL(req *http.Request) string {
	u := *req.URL
	u.RawQuery, u.User, u.Fragment = "", nil, ""
	return u.String()
}

// retryAfter parses a Retry-After header as seconds or an HTTP date.
func retryAfter(v string, now time.Time) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

// backoff is 1s, 2s, 4s, … with ±20% jitter.
func backoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 6)
	jitter := 0.8 + 0.4*rand.Float64() //nolint:gosec // jitter needs no crypto randomness
	return time.Duration(float64(d) * jitter)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting to retry: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
