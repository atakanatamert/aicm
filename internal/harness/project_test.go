// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProject(t *testing.T) {
	root := t.TempDir()

	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "internal", "harness")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("creating repo fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("creating .git directory: %v", err)
	}

	worktree := filepath.Join(root, "worktree")
	worktreeNested := filepath.Join(worktree, "src")
	if err := os.MkdirAll(worktreeNested, 0o700); err != nil {
		t.Fatalf("creating worktree fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
		t.Fatalf("creating .git file: %v", err)
	}

	missing := filepath.Join(root, "gone", "dir")

	// A linked worktree of a main repository counts as that repository.
	linked := filepath.Join(root, "linked")
	if err := os.MkdirAll(linked, 0o700); err != nil {
		t.Fatalf("creating linked worktree: %v", err)
	}
	gitdir := "gitdir: " + filepath.Join(root, "mainrepo", ".git", "worktrees", "linked") + "\n"
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte(gitdir), 0o600); err != nil {
		t.Fatalf("creating linked .git file: %v", err)
	}

	// Loose folders directly under the home directory are not projects.
	fakeHome := filepath.Join(root, "home")
	desktop := filepath.Join(fakeHome, "Desktop")
	if err := os.MkdirAll(desktop, 0o700); err != nil {
		t.Fatalf("creating fake home: %v", err)
	}

	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", fakeHome)
	} else {
		t.Setenv("HOME", fakeHome)
	}
	home := fakeHome

	// On Windows the root of a volume (C:\) counts as the root as well.
	volumeRoot := string(filepath.Separator)
	if vol := filepath.VolumeName(root); vol != "" {
		volumeRoot = vol + string(filepath.Separator)
	}

	tests := []struct {
		name string
		cwd  string
		want string
	}{
		{name: "repository root", cwd: repo, want: "repo"},
		{name: "nested directory", cwd: nested, want: "repo"},
		{name: "worktree .git file", cwd: worktreeNested, want: "worktree"},
		{name: "missing path", cwd: missing, want: "dir"},
		{name: "empty input", cwd: "", want: ""},
		{name: "root", cwd: string(filepath.Separator), want: ""},
		{name: "volume root", cwd: volumeRoot, want: ""},
		{name: "home", cwd: home, want: ""},
		{name: "temp dir", cwd: os.TempDir(), want: ""},
		{name: "linked worktree", cwd: linked, want: "mainrepo"},
		{name: "loose folder under home", cwd: desktop, want: ""},
		{name: "tmp", cwd: "/tmp", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Project(test.cwd)
			if got != test.want {
				t.Fatalf("Project(%q) = %q, want %q", test.cwd, got, test.want)
			}
			if got != "" && strings.Contains(got, string(filepath.Separator)) {
				t.Fatalf("Project(%q) returned a path %q, want a base name", test.cwd, got)
			}
			// A second call is served from the cache and must agree.
			if again := Project(test.cwd); again != got {
				t.Fatalf("cached Project(%q) = %q, want %q", test.cwd, again, got)
			}
		})
	}
}
