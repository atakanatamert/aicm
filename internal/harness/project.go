// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// projectCache remembers the project name for working directories that repeat
// in every log line of a session.
var projectCache = struct {
	sync.Mutex
	byCwd map[string]string
}{byCwd: map[string]string{}}

// Project returns the base name of the git repository containing cwd (the
// main repository for a linked worktree), or the base name of cwd itself when
// it is not inside a repository. It never returns a full path. An empty cwd,
// /, the home directory, temp directories and loose folders directly under
// the home directory (Desktop, .config) give "".
func Project(cwd string) string {
	if cwd == "" {
		return ""
	}
	key := filepath.Clean(cwd)
	projectCache.Lock()
	project, ok := projectCache.byCwd[key]
	projectCache.Unlock()
	if ok {
		return project
	}

	project = projectName(key)

	projectCache.Lock()
	projectCache.byCwd[key] = project
	projectCache.Unlock()
	return project
}

func projectName(cwd string) string {
	home := ""
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		home = filepath.Clean(h)
	}
	if cwd == string(filepath.Separator) || cwd == home || isTempDir(cwd) {
		return ""
	}

	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		// The directory is gone (or was replaced by a file): fall back to
		// its name without walking a path that no longer exists.
		return looseName(cwd, home)
	}

	// Walk up to the repository root, but never into the home directory or
	// above it: a dotfiles repository in ~ is not every session's project.
	for dir := cwd; dir != home; {
		gitPath := filepath.Join(dir, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			if !fi.IsDir() {
				// A .git file marks a linked worktree; name the main
				// repository so all its worktrees count as one project.
				if main := worktreeMain(gitPath); main != "" {
					return main
				}
			}
			return filepath.Base(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return looseName(cwd, home)
}

// looseName names a directory outside any repository: its base name, except
// directly under the home directory (Desktop, .config) or in a temp
// directory, where the name says nothing about a project.
func looseName(cwd, home string) string {
	parent := filepath.Dir(cwd)
	if parent == home || parent == string(filepath.Separator) || isTempDir(parent) || isTempDir(cwd) {
		return ""
	}
	return filepath.Base(cwd)
}

func isTempDir(dir string) bool {
	switch dir {
	case filepath.Clean(os.TempDir()), "/tmp", "/private/tmp", "/var/tmp":
		return true
	}
	return false
}

// worktreeMain reads a worktree's .git file ("gitdir: <repo>/.git/worktrees/<name>")
// and returns <repo>'s base name, or "" when the file has another shape.
func worktreeMain(gitFile string) string {
	data, err := os.ReadFile(gitFile) //nolint:gosec // a .git file inside the session's own working tree
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	gitdir, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	gitdir = filepath.Clean(strings.TrimSpace(gitdir))
	sep := string(filepath.Separator)
	marker := sep + ".git" + sep + "worktrees" + sep
	i := strings.Index(gitdir, marker)
	if i <= 0 {
		return ""
	}
	return filepath.Base(gitdir[:i])
}
