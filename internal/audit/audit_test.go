// SPDX-License-Identifier: SUL-1.0

package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// The audit log must never store a credential, however a caller nests it.
func TestEncodeRedactsSecrets(t *testing.T) {
	in := map[string]any{
		"name":   "OpenRouter",
		"secret": "sk-or-v1-abcdef",
		"plan":   map[string]any{"price": 2000, "api_key": "sk-live-123"},
		"smtp":   []any{map[string]any{"host": "smtp.example.com", "password": "hunter2hunter2"}},
		"tokens": nil,
	}
	b, err := encode(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, leaked := range []string{"sk-or-v1-abcdef", "sk-live-123", "hunter2hunter2"} {
		if strings.Contains(s, leaked) {
			t.Errorf("audit JSON leaks %q: %s", leaked, s)
		}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "OpenRouter" || out["plan"].(map[string]any)["price"] != float64(2000) {
		t.Errorf("non-secret fields changed: %s", s)
	}
	if out["tokens"] != nil {
		t.Errorf("nil secret-named field should stay null: %s", s)
	}
}
