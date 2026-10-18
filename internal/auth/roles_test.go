// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestOperationRolesMatchSpec(t *testing.T) {
	f, err := os.Open("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("opening spec: %v", err)
	}
	defer f.Close()

	seen := make(map[string]bool)
	var pending string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if id, ok := strings.CutPrefix(line, "operationId:"); ok {
			if pending != "" {
				t.Errorf("operation %q has no x-aicm-role", pending)
			}
			pending = strings.TrimSpace(id)
			continue
		}
		role, ok := strings.CutPrefix(line, "x-aicm-role:")
		if !ok || pending == "" {
			continue
		}
		want := strings.TrimSpace(role)
		if want == "public" {
			want = ""
		}
		got, known := OperationRoles[pending]
		if !known {
			t.Errorf("OperationRoles is missing %q", pending)
		} else if string(got) != want {
			t.Errorf("OperationRoles[%q] = %q, spec says %q", pending, got, want)
		}
		seen[pending] = true
		pending = ""
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	if pending != "" {
		t.Errorf("operation %q has no x-aicm-role", pending)
	}
	for id := range OperationRoles {
		if !seen[id] {
			t.Errorf("OperationRoles has extra key %q not in the spec", id)
		}
	}
}

func TestRoleAtLeast(t *testing.T) {
	if !RoleViewer.AtLeast(RoleViewer) || !RoleEditor.AtLeast(RoleViewer) || !RoleAdmin.AtLeast(RoleEditor) {
		t.Fatal("a role should grant itself and weaker roles")
	}
	if RoleViewer.AtLeast(RoleEditor) {
		t.Fatal("viewer must not satisfy editor")
	}
	if RoleEditor.AtLeast(RoleAdmin) {
		t.Fatal("editor must not satisfy admin")
	}
	unknown := Role("superuser")
	if unknown.AtLeast(RoleViewer) || unknown.AtLeast("") {
		t.Fatal("unknown role must not satisfy any requirement")
	}
}
