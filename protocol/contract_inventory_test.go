package protocol

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestSpecContractInventoryIsClosed(t *testing.T) {
	wantFiles := []string{
		"CHANGELOG.md",
		"VERSION",
		"errors.md",
		"kolk.openapi.yaml",
		"provider-usage.md",
		"schemas/envelope.json",
		"stdio.md",
		"testdata/foreign/README.md",
		"testdata/foreign/claude-plain.ndjson",
		"testdata/foreign/claude-tool-use.ndjson",
	}
	for _, command := range KnownCommandTypes() {
		wantFiles = append(wantFiles,
			"schemas/commands/"+string(command)+".json",
			"testdata/commands/"+string(command)+".json",
		)
	}
	for _, entity := range []string{"error", "score", "usage"} {
		wantFiles = append(wantFiles,
			"schemas/entities/"+entity+".json",
			"testdata/entities/"+entity+".json",
		)
	}
	for _, event := range KnownEventTypes() {
		wantFiles = append(wantFiles,
			"schemas/events/"+string(event)+".json",
			"testdata/events/"+string(event)+".json",
		)
	}
	for name := range wholeTurnFixtureTypes {
		wantFiles = append(wantFiles,
			"testdata/streams/"+name+".ndjson",
			"testdata/streams/"+name+".sse",
		)
	}
	sort.Strings(wantFiles)

	wantDirectories := []string{
		"schemas",
		"schemas/commands",
		"schemas/entities",
		"schemas/events",
		"testdata",
		"testdata/commands",
		"testdata/entities",
		"testdata/events",
		"testdata/foreign",
		"testdata/streams",
	}
	sort.Strings(wantDirectories)

	root := filepath.Join("..", "spec")
	var gotFiles, gotDirectories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			gotDirectories = append(gotDirectories, relative)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Errorf("irregular contract artifact %s has mode %s", relative, info.Mode())
			return nil
		}
		gotFiles = append(gotFiles, relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(gotFiles)
	sort.Strings(gotDirectories)
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Errorf("spec file inventory =\n%v\nwant =\n%v", gotFiles, wantFiles)
	}
	if !reflect.DeepEqual(gotDirectories, wantDirectories) {
		t.Errorf("spec directory inventory =\n%v\nwant =\n%v", gotDirectories, wantDirectories)
	}

	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changelog) == 0 {
		t.Error("spec/CHANGELOG.md must not be empty")
	}
}
