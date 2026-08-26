package engine_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestDetectQualityGates(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "empty project",
			files: map[string]string{},
			want:  nil,
		},
		{
			name:  "go project",
			files: map[string]string{"go.mod": "module example\n"},
			want:  []string{"go vet ./... && go test ./..."},
		},
		{
			name:  "node project",
			files: map[string]string{"package.json": "{}\n"},
			want:  []string{"npm test"},
		},
		{
			name:  "rust project",
			files: map[string]string{"Cargo.toml": "[package]\n"},
			want:  []string{"cargo test"},
		},
		{
			name:  "make check takes precedence",
			files: map[string]string{"Makefile": "test:\n\tgo test ./...\ncheck:\n\tmake test\n"},
			want:  []string{"make check"},
		},
		{
			name:  "make test fallback",
			files: map[string]string{"Makefile": "test:\n\tgo test ./...\n"},
			want:  []string{"make test"},
		},
		{
			name: "all supported project files preserve stable order",
			files: map[string]string{
				"go.mod":       "module example\n",
				"package.json": "{}\n",
				"Cargo.toml":   "[package]\n",
				"Makefile":     "check:\n\ttrue\n",
			},
			want: []string{
				"go vet ./... && go test ./...",
				"npm test",
				"cargo test",
				"make check",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := engine.DetectQualityGates(dir); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("DetectQualityGates() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDetectQualityGatesEmptyPath(t *testing.T) {
	if got := engine.DetectQualityGates(" "); got != nil {
		t.Fatalf("DetectQualityGates(empty) = %#v, want nil", got)
	}
}
