package local

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const runtimeReleaseURL = "https://api.github.com/repos/ollama/ollama/releases/latest"

type runtimePlatform struct{ Asset, Format, Binary string }

// Names describe release packaging, not a pinned version or model catalog.
var runtimePlatforms = map[string]runtimePlatform{
	"darwin/arm64": {"ollama-darwin.tgz", "tgz", "ollama"},
	"darwin/amd64": {"ollama-darwin.tgz", "tgz", "ollama"},
	"linux/amd64":  {"ollama-linux-amd64.tar.zst", "tar.zst", "bin/ollama"},
	"linux/arm64":  {"ollama-linux-arm64.tar.zst", "tar.zst", "bin/ollama"},
}

type runtimeRelease struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []runtimeAsset `json:"assets"`
}

type runtimeAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

type selectedRuntimeRelease struct {
	Tag   string
	Asset runtimeAsset
	// Companion is the official bundle extracted over Asset, from the same
	// release and trusted the same way; nil when the machine needs none.
	Companion *runtimeAsset
}

func (i *RuntimeInstaller) release(ctx context.Context, platform runtimePlatform) (selectedRuntimeRelease, error) {
	return i.releaseFor(ctx, platform, runtimePlatform{})
}

// releaseFor selects the standard bundle and, when companion names one, its
// companion from the same stable release. Either failing refuses both.
func (i *RuntimeInstaller) releaseFor(ctx context.Context, platform, companion runtimePlatform) (selectedRuntimeRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := i.get(ctx, runtimeReleaseURL)
	if err != nil {
		return selectedRuntimeRelease{}, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(fetchReader{response.Body}, (2<<20)+1))
	if err != nil {
		return selectedRuntimeRelease{}, err
	}
	if len(data) > 2<<20 {
		return selectedRuntimeRelease{}, fmt.Errorf("official runtime release metadata exceeds size limit")
	}
	var release runtimeRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return selectedRuntimeRelease{}, fmt.Errorf("reading official runtime release: %w", err)
	}
	if release.Draft || release.Prerelease || !validRuntimeTag(release.Tag) {
		return selectedRuntimeRelease{}, fmt.Errorf("official runtime release is not a valid stable version")
	}
	selected, err := trustedRuntimeAsset(release, platform.Asset)
	if err != nil {
		return selectedRuntimeRelease{}, err
	}
	chosen := selectedRuntimeRelease{Tag: release.Tag, Asset: selected}
	if companion.Asset != "" {
		extra, err := trustedRuntimeAsset(release, companion.Asset)
		if err != nil {
			return selectedRuntimeRelease{}, err
		}
		chosen.Companion = &extra
	}
	return chosen, nil
}

// trustedRuntimeAsset finds exactly one asset named name in release and
// checks its official URL for this tag, its size bound and its SHA-256 digest.
func trustedRuntimeAsset(release runtimeRelease, name string) (runtimeAsset, error) {
	var selected *runtimeAsset
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if selected != nil {
			return runtimeAsset{}, fmt.Errorf("official runtime release repeats asset %s", name)
		}
		selected = &asset
	}
	if selected == nil {
		return runtimeAsset{}, fmt.Errorf("official runtime release %s has no %s bundle", release.Tag, name)
	}
	wantURL := "https://github.com/ollama/ollama/releases/download/" + release.Tag + "/" + name
	if selected.URL != wantURL || selected.Size <= 0 || selected.Size > runtimeArchiveLimits.Compressed {
		return runtimeAsset{}, fmt.Errorf("official runtime asset %s has an invalid origin or size", name)
	}
	digest, ok := strings.CutPrefix(selected.Digest, "sha256:")
	decoded, err := hex.DecodeString(digest)
	if !ok || err != nil || len(decoded) != 32 {
		return runtimeAsset{}, fmt.Errorf("official runtime asset %s has no valid SHA-256 digest", name)
	}
	selected.Digest = strings.ToLower(digest)
	return *selected, nil
}

func validRuntimeTag(tag string) bool {
	if len(tag) < 2 || len(tag) > 100 || tag[0] != 'v' || tag[1] < '0' || tag[1] > '9' {
		return false
	}
	for _, r := range tag[1:] {
		valid := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || strings.ContainsRune("._-", r)
		if !valid {
			return false
		}
	}
	return true
}

func runtimeOrigin(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

// runtimeFetchError is a failure to reach or read the official release: no
// network, a proxy, a dropped connection, a deadline. Only this earns "check
// the network" in setup's advice; a retry of anything else fails the same way.
type runtimeFetchError struct{ err error }

func (e *runtimeFetchError) Error() string { return e.err.Error() }
func (e *runtimeFetchError) Unwrap() error { return e.err }

// fetchReader marks a response body's read errors as fetch failures. io.EOF
// stays as it is: readers compare it, not unwrap it.
type fetchReader struct{ r io.Reader }

func (f fetchReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err != nil && err != io.EOF {
		err = &runtimeFetchError{err}
	}
	return n, err
}

func (i *RuntimeInstaller) get(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil || !runtimeOrigin(req.URL) {
		return nil, fmt.Errorf("runtime download requires an official HTTPS origin")
	}
	req.Header.Set("User-Agent", "kolkrabbi-localia")
	client := http.Client{Timeout: 45 * time.Minute}
	if i.client != nil {
		client = *i.client
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !runtimeOrigin(req.URL) {
			return fmt.Errorf("runtime download redirected outside official HTTPS origins or too many times")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		// Signed CDN query parameters must not appear in terminal errors.
		var urlErr *url.Error
		for errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, &runtimeFetchError{fmt.Errorf("requesting official Ollama runtime: %w", err)}
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("official runtime download returned HTTP %d; retry setup later", response.StatusCode)
	}
	return response, nil
}
