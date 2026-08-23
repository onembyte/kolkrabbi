package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testArchiveMember struct {
	name     string
	typeflag byte
	body     []byte
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func makeReleaseArchive(t *testing.T, members ...testArchiveMember) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	for _, member := range members {
		typeflag := member.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name: member.name, Typeflag: typeflag, Mode: 0o755,
		}
		if typeflag == tar.TypeReg {
			header.Size = int64(len(member.body))
		}
		if typeflag == tar.TypeSymlink || typeflag == tar.TypeLink {
			header.Linkname = "README.md"
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write(member.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func validReleaseArchive(t *testing.T) []byte {
	return makeReleaseArchive(t,
		testArchiveMember{name: "kolk", body: []byte("verified executable")},
		testArchiveMember{name: "README.md", body: []byte("read me")},
		testArchiveMember{name: "LICENSE", body: []byte("licensed")},
	)
}

func declaredOversizeArchive(t *testing.T) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "kolk", Typeflag: tar.TypeReg, Mode: 0o755, Size: maxBinaryBytes + 1,
	}); err != nil {
		t.Fatal(err)
	}
	// The verifier rejects the declared size before reading member data. Do
	// not close tw: a correct tar writer refuses to finish an intentionally
	// incomplete oversized fixture.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func checksumRow(name string, body []byte) string {
	return fmt.Sprintf("%x  %s\n", sha256.Sum256(body), name)
}

func TestFetchVerifiedArtifactUsesExactPathsAndReturnsBinary(t *testing.T) {
	archive := validReleaseArchive(t)
	version, _ := parseStableVersion("1.2.3")
	target, _ := resolveTarget("darwin", "arm64")
	name := target.archiveName(version)
	wantPaths := []string{
		"/releases/download/v1.2.3/checksums.txt",
		"/releases/download/v1.2.3/" + name,
	}
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		switch r.URL.Path {
		case wantPaths[0]:
			_, _ = io.WriteString(w, checksumRow(name, archive))
		case wantPaths[1]:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := fetchVerifiedArtifact(context.Background(), srv.Client(), srv.URL+"/releases", version, target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "verified executable" {
		t.Fatalf("binary = %q", got)
	}
	if fmt.Sprint(gotPaths) != fmt.Sprint(wantPaths) {
		t.Fatalf("request paths = %v, want %v", gotPaths, wantPaths)
	}
}

func TestChecksumManifestRequiresOneExactLowercaseEntry(t *testing.T) {
	name := "kolk_1.2.3_linux_amd64.tar.gz"
	valid := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name     string
		manifest string
	}{
		{"missing", strings.Repeat("b", 64) + "  another.tar.gz\n"},
		{"duplicate", valid + "  " + name + "\n" + valid + "  " + name + "\n"},
		{"uppercase", strings.Repeat("A", 64) + "  " + name + "\n"},
		{"short", "abc  " + name + "\n"},
		{"non hex", strings.Repeat("g", 64) + "  " + name + "\n"},
		{"extra field", valid + "  " + name + " ignored\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := checksumFor([]byte(tc.manifest), name); err == nil {
				t.Fatalf("manifest %q was accepted", tc.manifest)
			}
		})
	}

	digest, err := checksumFor([]byte(valid+"  "+name+"\n"), name)
	if err != nil || fmt.Sprintf("%x", digest) != valid {
		t.Fatalf("valid checksum = (%x, %v)", digest, err)
	}
}

func TestInvalidManifestDoesNotRequestArchive(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(w, "no matching checksum\n")
	}))
	defer srv.Close()
	version, _ := parseStableVersion("1.2.3")
	target, _ := resolveTarget("linux", "amd64")

	if _, err := fetchVerifiedArtifact(context.Background(), srv.Client(), srv.URL+"/releases", version, target); err == nil {
		t.Fatal("invalid manifest succeeded")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, invalid manifest must stop before archive", requests)
	}
}

func TestArtifactDigestIsCheckedBeforeDecompression(t *testing.T) {
	archive := []byte("not a gzip archive")
	version, _ := parseStableVersion("1.2.3")
	target, _ := resolveTarget("linux", "arm64")
	name := target.archiveName(version)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = io.WriteString(w, strings.Repeat("0", 64)+"  "+name+"\n")
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	_, err := fetchVerifiedArtifact(context.Background(), srv.Client(), srv.URL+"/releases", version, target)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") || strings.Contains(err.Error(), "gzip") {
		t.Fatalf("verification error = %v", err)
	}
}

func TestDownloadBoundedRejectsStatusDeclaredAndStreamedOversize(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		if _, err := downloadBounded(context.Background(), srv.Client(), srv.URL, 8); err == nil ||
			!strings.Contains(err.Error(), "503") {
			t.Fatalf("status error = %v", err)
		}
	})

	t.Run("declared size", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "9")
			_, _ = io.WriteString(w, "tiny")
		}))
		defer srv.Close()
		if _, err := downloadBounded(context.Background(), srv.Client(), srv.URL, 8); err == nil {
			t.Fatal("declared oversized response succeeded")
		}
	})

	t.Run("streamed size", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "123456789")
		}))
		defer srv.Close()
		if _, err := downloadBounded(context.Background(), srv.Client(), srv.URL, 8); err == nil {
			t.Fatal("streamed oversized response succeeded")
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := downloadBounded(ctx, http.DefaultClient, "https://example.invalid", 8); err == nil {
			t.Fatal("cancelled download succeeded")
		}
	})

	t.Run("closes body", func(t *testing.T) {
		body := &trackingBody{Reader: strings.NewReader("small")}
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, ContentLength: 5, Body: body, Request: req,
			}, nil
		})}
		if _, err := downloadBounded(context.Background(), client, "https://example.test/artifact", 8); err != nil {
			t.Fatal(err)
		}
		if !body.closed {
			t.Fatal("successful bounded download did not close its response body")
		}
	})
}

func TestVerifyReleaseArchiveRejectsUnsafeShapes(t *testing.T) {
	regular := func(name, body string) testArchiveMember {
		return testArchiveMember{name: name, body: []byte(body)}
	}
	valid := []testArchiveMember{regular("kolk", "binary"), regular("README.md", "readme"), regular("LICENSE", "license")}
	cases := []struct {
		name    string
		archive func(*testing.T) []byte
	}{
		{"not gzip", func(*testing.T) []byte { return []byte("garbage") }},
		{"truncated", func(t *testing.T) []byte {
			archive := validReleaseArchive(t)
			return archive[:len(archive)/2]
		}},
		{"oversized member", declaredOversizeArchive},
		{"concatenated gzip", func(t *testing.T) []byte {
			first := validReleaseArchive(t)
			return append(first, validReleaseArchive(t)...)
		}},
		{"missing", func(t *testing.T) []byte { return makeReleaseArchive(t, valid[:2]...) }},
		{"extra", func(t *testing.T) []byte { return makeReleaseArchive(t, append(valid, regular("surprise", "x"))...) }},
		{"duplicate", func(t *testing.T) []byte { return makeReleaseArchive(t, append(valid, regular("kolk", "again"))...) }},
		{"symlink", func(t *testing.T) []byte {
			members := append([]testArchiveMember(nil), valid...)
			members[0] = testArchiveMember{name: "kolk", typeflag: tar.TypeSymlink}
			return makeReleaseArchive(t, members...)
		}},
		{"hard link", func(t *testing.T) []byte {
			members := append([]testArchiveMember(nil), valid...)
			members[0] = testArchiveMember{name: "kolk", typeflag: tar.TypeLink}
			return makeReleaseArchive(t, members...)
		}},
		{"prefixed", func(t *testing.T) []byte {
			members := append([]testArchiveMember(nil), valid...)
			members[0].name = "./kolk"
			return makeReleaseArchive(t, members...)
		}},
		{"empty binary", func(t *testing.T) []byte {
			members := append([]testArchiveMember(nil), valid...)
			members[0].body = nil
			return makeReleaseArchive(t, members...)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifiedBinary(tc.archive(t)); err == nil {
				t.Fatal("unsafe archive succeeded")
			}
		})
	}
	if _, err := verifiedBinary(declaredOversizeArchive(t)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized member error = %v", err)
	}

	got, err := verifiedBinary(makeReleaseArchive(t, valid...))
	if err != nil || string(got) != "binary" {
		t.Fatalf("valid archive = (%q, %v)", got, err)
	}
}
