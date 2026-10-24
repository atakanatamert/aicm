// SPDX-License-Identifier: SUL-1.0

package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/atakanatamert/aicm/internal/version"
)

// releaseRepo is the project's GitHub repository. A rename updates this
// constant and version.Name.
const releaseRepo = "atakanatamert/aicm"

const (
	releaseAPI       = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	releasesAPI      = "https://api.github.com/repos/" + releaseRepo + "/releases?per_page=100"
	checksumsAsset   = "checksums.txt"
	maxDownloadBytes = 512 << 20
)

type release struct {
	TagName     string         `json:"tag_name"`
	Draft       bool           `json:"draft"`
	PublishedAt string         `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Update downloads the latest release if it is newer than version.Version,
// verifies its SHA-256 against checksums.txt and replaces the running
// executable atomically. It returns the new version, or "" when current.
func Update(ctx context.Context, client *http.Client) (string, error) {
	if version.Version == "dev" {
		return "", nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	rel, err := latestRelease(ctx, client)
	if err != nil {
		return "", err
	}
	if rel == nil {
		return "", nil
	}
	newer, err := versionIsNewer(rel.TagName, version.Version)
	if err != nil {
		return "", err
	}
	if !newer {
		return "", nil
	}

	tag := strings.TrimPrefix(rel.TagName, "v")
	name := fmt.Sprintf("%s_%s_%s_%s", version.AgentBinary, tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".zip"
	} else {
		name += ".tar.gz"
	}
	assetURL, sumsURL := findAsset(rel.Assets, name), findAsset(rel.Assets, checksumsAsset)
	if assetURL == "" {
		return "", fmt.Errorf("release %s has no asset %s", rel.TagName, name)
	}
	if sumsURL == "" {
		return "", fmt.Errorf("release %s has no %s", rel.TagName, checksumsAsset)
	}
	archive, err := download(ctx, client, assetURL)
	if err != nil {
		return "", err
	}
	sums, err := download(ctx, client, sumsURL)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return "", fmt.Errorf("checksum mismatch for %s", name)
	}
	binary, err := extractBinary(name, archive)
	if err != nil {
		return "", err
	}
	if err := replaceExecutable(binary); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// latestRelease returns the stable latest release, or while none exists the
// most recently published non-draft release. It returns nil when there is no
// published release at all. The list endpoint is not in publication order, so
// the greatest published_at (ISO 8601 UTC, sorts lexicographically) wins.
func latestRelease(ctx context.Context, client *http.Client) (*release, error) {
	body, err := download(ctx, client, releaseAPI)
	if err == nil {
		var rel release
		if err := json.Unmarshal(body, &rel); err != nil {
			return nil, fmt.Errorf("decoding release: %w", err)
		}
		if rel.TagName == "" {
			return nil, errors.New("release has no tag name")
		}
		return &rel, nil
	}
	if !errors.Is(err, errNotFound) {
		return nil, err
	}
	body, err = download(ctx, client, releasesAPI)
	if err != nil {
		return nil, err
	}
	var rels []release
	if err := json.Unmarshal(body, &rels); err != nil {
		return nil, fmt.Errorf("decoding release list: %w", err)
	}
	var newest *release
	for i := range rels {
		r := &rels[i]
		if r.Draft || r.PublishedAt == "" || r.TagName == "" {
			continue
		}
		if newest == nil || r.PublishedAt > newest.PublishedAt {
			newest = r
		}
	}
	return newest, nil
}

// findAsset returns an asset's download URL by exact name.
func findAsset(assets []releaseAsset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// checksumFor parses GoReleaser's "<sha256>  <filename>" lines.
func checksumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s has no checksum for %s", checksumsAsset, name)
}

// errNotFound marks a 404, which the release API returns while no stable
// release exists.
var errNotFound = errors.New("not found")

// download fetches one URL with a size cap.
func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", version.AgentBinary+"/"+version.Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do with a close error
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("requesting %s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("requesting %s: %s: %s", url, resp.Status, strings.TrimSpace(string(msg)))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(b) > maxDownloadBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, maxDownloadBytes)
	}
	return b, nil
}

// versionIsNewer reports whether latest is a newer semantic-ish version than
// current. Both may carry a leading v. A final release is newer than a
// pre-release of the same number (v1.2.0 > v1.2.0-rc.1).
func versionIsNewer(latest, current string) (bool, error) {
	l, err := versionParts(latest)
	if err != nil {
		return false, fmt.Errorf("parsing release version %q: %w", latest, err)
	}
	c, err := versionParts(current)
	if err != nil {
		return false, fmt.Errorf("parsing current version %q: %w", current, err)
	}
	for i := range max(len(l), len(c)) {
		var a, b int
		if i < len(l) {
			a = l[i]
		}
		if i < len(c) {
			b = c[i]
		}
		if a != b {
			return a > b, nil
		}
	}
	return isPrerelease(current) && !isPrerelease(latest), nil
}

func isPrerelease(v string) bool {
	return strings.Contains(v, "-")
}

// versionParts turns v1.2.3 into [1 2 3], ignoring a prerelease suffix.
func versionParts(v string) ([]int, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, errors.New("empty version")
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("parsing version segment %q: %w", f, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// extractBinary pulls the agent executable out of its release archive.
func extractBinary(name string, data []byte) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractZip(data)
	}
	return extractTarGz(data)
}

func extractTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close() //nolint:errcheck // reading already finished
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != version.AgentBinary {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxDownloadBytes))
		if err != nil {
			return nil, fmt.Errorf("reading %s from archive: %w", version.AgentBinary, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s not found in archive", version.AgentBinary)
}

func extractZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening zip archive: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != version.AgentBinary {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s in archive: %w", version.AgentBinary, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxDownloadBytes))
		closeErr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s from archive: %w", version.AgentBinary, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("closing %s in archive: %w", version.AgentBinary, closeErr)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s not found in archive", version.AgentBinary)
}

// replaceExecutable writes the new binary next to the running one and renames
// it into place.
func replaceExecutable(binary []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, "."+version.AgentBinary+"-update-*")
	if err != nil {
		return fmt.Errorf("creating update file: %w", err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(binary); err != nil {
		cleanup()
		return fmt.Errorf("writing update file: %w", err)
	}
	if err := f.Chmod(0o755); err != nil {
		cleanup()
		return fmt.Errorf("setting update file mode: %w", err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing update file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing update file: %w", err)
	}
	if runtime.GOOS == "windows" {
		// Windows cannot replace a running executable, but can rename it.
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("moving current executable aside: %w", err)
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing executable: %w", err)
	}
	return nil
}
