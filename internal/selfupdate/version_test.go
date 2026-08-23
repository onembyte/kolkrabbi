package selfupdate

import "testing"

func TestParseStableVersion(t *testing.T) {
	valid := map[string]string{
		"0.1.0":    "0.1.0",
		"v2.10.3":  "2.10.3",
		"12.0.400": "12.0.400",
	}
	for input, want := range valid {
		t.Run("valid_"+input, func(t *testing.T) {
			got, err := parseStableVersion(input)
			if err != nil || got.String() != want {
				t.Fatalf("parseStableVersion(%q) = (%q, %v), want %q", input, got, err, want)
			}
		})
	}

	for _, input := range []string{
		"", "dev", "1", "1.2", "1.2.3.4", "1.2.3-beta", "1.2.3+meta",
		"01.2.3", "1.02.3", "1.2.03", "v", "vv1.2.3", "-1.2.3", "1.2.x",
		"18446744073709551616.0.0",
	} {
		t.Run("invalid_"+input, func(t *testing.T) {
			if _, err := parseStableVersion(input); err == nil {
				t.Fatalf("parseStableVersion(%q) succeeded", input)
			}
		})
	}
}

func TestStableVersionComparisonIsNumeric(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"0.9.10", "0.10.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.10.0", "1.2.99", 1},
		{"2.0.0", "10.0.0", -1},
	}
	for _, tc := range cases {
		left, _ := parseStableVersion(tc.left)
		right, _ := parseStableVersion(tc.right)
		if got := left.Compare(right); got != tc.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", left, right, got, tc.want)
		}
	}
}

func TestResolveReleaseTargetAndArchiveName(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		want         string
	}{
		{"darwin", "amd64", "kolk_1.2.3_darwin_amd64.tar.gz"},
		{"darwin", "arm64", "kolk_1.2.3_darwin_arm64.tar.gz"},
		{"linux", "amd64", "kolk_1.2.3_linux_amd64.tar.gz"},
		{"linux", "arm64", "kolk_1.2.3_linux_arm64.tar.gz"},
	} {
		t.Run(tc.goos+"_"+tc.goarch, func(t *testing.T) {
			target, err := resolveTarget(tc.goos, tc.goarch)
			if err != nil {
				t.Fatal(err)
			}
			version, _ := parseStableVersion("1.2.3")
			if got := target.archiveName(version); got != tc.want {
				t.Fatalf("archiveName = %q, want %q", got, tc.want)
			}
		})
	}

	for _, tc := range [][2]string{{"windows", "amd64"}, {"freebsd", "arm64"}, {"linux", "386"}, {"darwin", "aarch64"}} {
		if _, err := resolveTarget(tc[0], tc[1]); err == nil {
			t.Errorf("resolveTarget(%q, %q) succeeded", tc[0], tc[1])
		}
	}
}
