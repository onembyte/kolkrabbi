package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const officialReleasesURL = "https://github.com/onembyte/kolkrabbi/releases"

func discoverLatest(ctx context.Context, client *http.Client, releasesURL string) (stableVersion, error) {
	base, err := releaseOrigin(releasesURL)
	if err != nil {
		return stableVersion{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base.String()+"/latest", nil)
	if err != nil {
		return stableVersion{}, fmt.Errorf("creating latest-release request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return stableVersion{}, fmt.Errorf("discovering latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return stableVersion{}, fmt.Errorf("discovering latest release: HTTP %d", resp.StatusCode)
	}

	final := resp.Request.URL
	if final.Scheme != base.Scheme || final.Host != base.Host || final.User != nil ||
		final.RawQuery != "" || final.Fragment != "" || final.RawPath != "" {
		return stableVersion{}, fmt.Errorf("latest release redirected to an unexpected URL")
	}
	prefix := base.Path + "/tag/v"
	if !strings.HasPrefix(final.Path, prefix) {
		return stableVersion{}, fmt.Errorf("latest release redirected to an unexpected URL")
	}
	raw := strings.TrimPrefix(final.Path, prefix)
	version, err := parseStableVersion(raw)
	if err != nil || final.Path != prefix+version.String() {
		return stableVersion{}, fmt.Errorf("latest release is not a stable semantic version")
	}
	return version, nil
}

func releaseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, fmt.Errorf("invalid release origin %q", raw)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}
