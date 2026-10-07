package local

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type runtimeTransport func(*http.Request) (*http.Response, error)

func (f runtimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func runtimeResponse(code int, body []byte) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), ContentLength: int64(len(body))}
}

func fixtureRuntimeRelease(platform string) runtimeRelease {
	spec := runtimePlatforms[platform]
	return runtimeRelease{Tag: "v99.12.3", Assets: []runtimeAsset{{Name: spec.Asset, Size: 123, Digest: "sha256:" + strings.Repeat("ab", 32), URL: "https://github.com/ollama/ollama/releases/download/v99.12.3/" + spec.Asset}}}
}

func TestRuntimeReleaseSelectionAndTrust(t *testing.T) {
	for platform := range runtimePlatforms {
		t.Run(platform, func(t *testing.T) {
			release := fixtureRuntimeRelease(platform)
			data, _ := json.Marshal(release)
			i := RuntimeInstaller{platform: platform, client: &http.Client{Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != runtimeReleaseURL {
					t.Errorf("unexpected metadata request: %s", r.URL)
				}
				return runtimeResponse(200, data), nil
			})}}
			got, err := i.release(context.Background(), runtimePlatforms[platform])
			if err != nil || got.Tag != release.Tag || got.Asset.Name != runtimePlatforms[platform].Asset {
				t.Fatalf("release = %+v, %v", got, err)
			}
		})
	}
	for _, bad := range []string{"missing digest", "bad digest", "duplicate", "draft", "prerelease", "foreign url", "http url", "escaped tag", "zero size", "huge size", "missing asset", "oversized metadata"} {
		t.Run(bad, func(t *testing.T) {
			r := fixtureRuntimeRelease("linux/amd64")
			switch bad {
			case "missing digest":
				r.Assets[0].Digest = ""
			case "bad digest":
				r.Assets[0].Digest = "sha256:" + strings.Repeat("z", 64)
			case "duplicate":
				r.Assets = append(r.Assets, r.Assets[0])
			case "draft":
				r.Draft = true
			case "prerelease":
				r.Prerelease = true
			case "foreign url":
				r.Assets[0].URL = "https://example.com/ollama"
			case "http url":
				r.Assets[0].URL = strings.Replace(r.Assets[0].URL, "https:", "http:", 1)
			case "escaped tag":
				r.Tag = "../../bad"
			case "zero size":
				r.Assets[0].Size = 0
			case "huge size":
				r.Assets[0].Size = runtimeArchiveLimits.Compressed + 1
			case "missing asset":
				r.Assets = nil
			}
			data, _ := json.Marshal(r)
			if bad == "oversized metadata" {
				data = append(data, []byte(strings.Repeat(" ", 2<<20))...)
			}
			i := RuntimeInstaller{client: &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) { return runtimeResponse(200, data), nil })}}
			if _, err := i.release(context.Background(), runtimePlatforms["linux/amd64"]); err == nil {
				t.Fatal("untrusted release accepted")
			}
		})
	}
}

// A companion comes from the same release as the standard bundle and passes
// the same checks: one asset, the exact official URL for that tag, a bounded
// size and a SHA-256 digest. Any failure refuses the whole release.
func TestRuntimeCompanionReleaseSelectionAndTrust(t *testing.T) {
	withCompanion := func(platform, name string) runtimeRelease {
		r := fixtureRuntimeRelease(platform)
		asset := runtimeCompanions[platform][name].Asset
		r.Assets = append(r.Assets, runtimeAsset{Name: asset, Size: 456, Digest: "sha256:" + strings.Repeat("CD", 32),
			URL: "https://github.com/ollama/ollama/releases/download/v99.12.3/" + asset})
		return r
	}
	installer := func(r runtimeRelease) RuntimeInstaller {
		data, _ := json.Marshal(r)
		return RuntimeInstaller{client: &http.Client{Transport: runtimeTransport(func(*http.Request) (*http.Response, error) {
			return runtimeResponse(200, data), nil
		})}}
	}
	for platform, companions := range runtimeCompanions {
		for name, companion := range companions {
			t.Run(platform+"/"+name, func(t *testing.T) {
				i := installer(withCompanion(platform, name))
				got, err := i.releaseFor(context.Background(), runtimePlatforms[platform], companion)
				if err != nil || got.Asset.Name != runtimePlatforms[platform].Asset || got.Companion == nil || got.Companion.Name != companion.Asset {
					t.Fatalf("release = %+v, %v; want the standard bundle and its companion", got, err)
				}
				if got.Companion.Digest != strings.Repeat("cd", 32) {
					t.Errorf("companion digest = %q, want the lowercase hex", got.Companion.Digest)
				}
			})
		}
	}
	t.Run("no companion requested", func(t *testing.T) {
		i := installer(withCompanion("linux/amd64", "rocm"))
		got, err := i.releaseFor(context.Background(), runtimePlatforms["linux/amd64"], runtimePlatform{})
		if err != nil || got.Companion != nil {
			t.Fatalf("release = %+v, %v; want no companion", got, err)
		}
	})
	rocm := runtimeCompanions["linux/amd64"]["rocm"]
	for _, bad := range []string{"missing", "duplicate", "foreign url", "other tag", "missing digest", "zero size", "huge size"} {
		t.Run("companion "+bad, func(t *testing.T) {
			r := withCompanion("linux/amd64", "rocm")
			c := &r.Assets[len(r.Assets)-1]
			switch bad {
			case "missing":
				r.Assets = r.Assets[:len(r.Assets)-1]
			case "duplicate":
				r.Assets = append(r.Assets, *c)
			case "foreign url":
				c.URL = "https://example.com/" + rocm.Asset
			case "other tag":
				c.URL = strings.Replace(c.URL, "v99.12.3", "v99.12.2", 1)
			case "missing digest":
				c.Digest = ""
			case "zero size":
				c.Size = 0
			case "huge size":
				c.Size = runtimeArchiveLimits.Compressed + 1
			}
			i := installer(r)
			_, err := i.releaseFor(context.Background(), runtimePlatforms["linux/amd64"], rocm)
			if err == nil {
				t.Fatal("untrusted companion accepted")
			}
			if bad == "missing" && !strings.Contains(err.Error(), rocm.Asset) {
				t.Errorf("error %q does not name the missing companion", err)
			}
		})
	}
}

func TestRuntimeHTTPRefusesRedirectToOtherOrigins(t *testing.T) {
	for _, target := range []string{"http://github.com/ollama", "https://example.com/ollama", "https://github.com.evil.test/file", "https://github.com:444/file", "https://user@github.com/file", "http://127.0.0.1:1234/private"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			i := RuntimeInstaller{client: &http.Client{Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := runtimeResponse(302, nil)
				resp.Header.Set("Location", target)
				return resp, nil
			})}}
			resp, err := i.get(context.Background(), runtimeReleaseURL)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil || calls != 1 {
				t.Fatalf("unsafe redirect followed: calls=%d, err=%v", calls, err)
			}
		})
	}
}
