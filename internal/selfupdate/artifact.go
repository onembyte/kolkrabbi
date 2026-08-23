package selfupdate

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxManifestBytes = 64 << 10
	maxArchiveBytes  = 64 << 20
	maxBinaryBytes   = 64 << 20
	maxReadmeBytes   = 2 << 20
	maxLicenseBytes  = 1 << 20
	maxExpandedBytes = maxBinaryBytes + maxReadmeBytes + maxLicenseBytes
)

func fetchVerifiedArtifact(
	ctx context.Context,
	client *http.Client,
	releasesURL string,
	version stableVersion,
	target releaseTarget,
) ([]byte, error) {
	base, err := releaseOrigin(releasesURL)
	if err != nil {
		return nil, err
	}
	archiveName := target.archiveName(version)
	downloadBase := fmt.Sprintf("%s/download/v%s", base, version)

	manifest, err := downloadBounded(ctx, client, downloadBase+"/checksums.txt", maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("downloading checksums.txt: %w", err)
	}
	wantDigest, err := checksumFor(manifest, archiveName)
	if err != nil {
		return nil, err
	}

	archive, err := downloadBounded(ctx, client, downloadBase+"/"+archiveName, maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", archiveName, err)
	}
	if got := sha256.Sum256(archive); got != wantDigest {
		return nil, fmt.Errorf("SHA-256 mismatch for %s", archiveName)
	}

	binary, err := verifiedBinary(archive)
	if err != nil {
		return nil, fmt.Errorf("validating %s: %w", archiveName, err)
	}
	return binary, nil
}

func downloadBounded(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("HTTP client is not configured")
	}
	if limit < 1 {
		return nil, fmt.Errorf("download size limit must be positive")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.Request == nil || resp.Request.URL == nil {
		return nil, fmt.Errorf("download response has no final URL")
	}
	if req.URL.Scheme == "https" && resp.Request.URL.Scheme != "https" {
		return nil, fmt.Errorf("download redirected away from HTTPS")
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response is larger than %d bytes", limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response is larger than %d bytes", limit)
	}
	return body, nil
}

func checksumFor(manifest []byte, archiveName string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	matches := 0
	scanner := bufio.NewScanner(bytes.NewReader(manifest))
	scanner.Buffer(make([]byte, 1024), maxManifestBytes)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[1] != archiveName {
			continue
		}
		matches++
		if len(fields) != 2 || len(fields[0]) != hex.EncodedLen(sha256.Size) || fields[0] != strings.ToLower(fields[0]) {
			return digest, fmt.Errorf("checksums.txt has an invalid SHA-256 entry for %s", archiveName)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return digest, fmt.Errorf("checksums.txt has an invalid SHA-256 entry for %s", archiveName)
		}
		copy(digest[:], decoded)
	}
	if err := scanner.Err(); err != nil {
		return digest, fmt.Errorf("reading checksums.txt: %w", err)
	}
	if matches != 1 {
		return digest, fmt.Errorf("checksums.txt has no unique SHA-256 entry for %s", archiveName)
	}
	return digest, nil
}

func verifiedBinary(archive []byte) ([]byte, error) {
	if len(archive) > maxArchiveBytes {
		return nil, fmt.Errorf("archive is larger than %d bytes", maxArchiveBytes)
	}
	compressed := bytes.NewReader(archive)
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	gz.Multistream(false)
	defer func() { _ = gz.Close() }()

	limits := map[string]int64{
		"kolk":      maxBinaryBytes,
		"README.md": maxReadmeBytes,
		"LICENSE":   maxLicenseBytes,
	}
	seen := make(map[string]bool, len(limits))
	var binary []byte
	var expanded int64
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar archive: %w", err)
		}
		limit, expected := limits[header.Name]
		if !expected {
			return nil, fmt.Errorf("archive contains unexpected path %q", header.Name)
		}
		if seen[header.Name] {
			return nil, fmt.Errorf("archive contains duplicate path %q", header.Name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return nil, fmt.Errorf("archive member %q is not a regular file", header.Name)
		}
		if header.Size < 0 || header.Size > limit || expanded > maxExpandedBytes-header.Size {
			return nil, fmt.Errorf("archive member %q is too large", header.Name)
		}
		if len(header.PAXRecords) != 0 {
			return nil, fmt.Errorf("archive member %q has unexpected extended metadata", header.Name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading archive member %q: %w", header.Name, err)
		}
		if int64(len(body)) != header.Size {
			return nil, fmt.Errorf("archive member %q is truncated", header.Name)
		}
		expanded += header.Size
		seen[header.Name] = true
		if header.Name == "kolk" {
			binary = body
		}
	}
	for name := range limits {
		if !seen[name] {
			return nil, fmt.Errorf("archive is missing %s", name)
		}
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("archive contains an empty kolk executable")
	}
	trailing, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("finishing gzip stream: %w", err)
	}
	if len(trailing) != 0 || compressed.Len() != 0 {
		return nil, fmt.Errorf("archive contains trailing or concatenated data")
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip stream: %w", err)
	}
	return binary, nil
}
