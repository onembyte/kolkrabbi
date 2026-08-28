package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Uninstalling has to be as easy as installing, and it has to say what it takes
// before it takes it. Nothing is removed without an explicit yes.
func TestUninstallRemovesNothingWithoutAYes(t *testing.T) {
	a, stdout, _ := newTestApp(t, "n\n")
	dirs := isolateHome(t)
	seed(t, dirs.Config, "config.json", `{"model":"x"}`)
	seed(t, dirs.Data, "credentials.json", `{"key":"secret"}`)

	if err := a.runUninstall(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Nothing was removed") {
		t.Errorf("a refusal was not reported plainly:\n%s", out)
	}
	for _, path := range []string{
		filepath.Join(dirs.Config, "config.json"),
		filepath.Join(dirs.Data, "credentials.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed despite the answer being no", path)
		}
	}
}

// End of input is not a yes. A closed stdin must never delete an API key.
func TestUninstallTreatsAClosedStdinAsNo(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	dirs := isolateHome(t)
	seed(t, dirs.Data, "credentials.json", `{"key":"secret"}`)

	if err := a.runUninstall(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dirs.Data, "credentials.json")); err != nil {
		t.Error("a closed stdin deleted the credentials")
	}
}

// It must say what it is about to delete, including the part people are
// surprised by.
func TestUninstallListsWhatItWillRemoveBeforeAsking(t *testing.T) {
	a, stdout, _ := newTestApp(t, "n\n")
	dirs := isolateHome(t)
	seed(t, dirs.Config, "config.json", `{}`)
	seed(t, dirs.Data, "credentials.json", `{}`)
	seed(t, dirs.Cache, "models.json", `[]`)

	if err := a.runUninstall(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, path := range []string{dirs.Config, dirs.Data, dirs.Cache} {
		if !strings.Contains(out, path) {
			t.Errorf("the listing does not mention %s:\n%s", path, out)
		}
	}
	if !strings.Contains(out, "your API key") {
		t.Errorf("the listing does not warn that the key goes with it:\n%s", out)
	}
}

func TestUninstallWithYesRemovesEverything(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	dirs := isolateHome(t)
	seed(t, dirs.Config, "config.json", `{}`)
	seed(t, dirs.Data, "credentials.json", `{}`)
	seed(t, dirs.Cache, "models.json", `[]`)

	if err := a.runUninstall(context.Background(), []string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{dirs.Config, dirs.Data, dirs.Cache} {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s survived an accepted uninstall", dir)
		}
	}
	if !strings.Contains(stdout.String(), "uninstalled") {
		t.Errorf("completion was not reported:\n%s", stdout.String())
	}
}

// --keep-data is the difference between "I am done with this" and "I am
// reinstalling": the key and the sessions stay.
func TestUninstallKeepDataSparesTheKeyAndSessions(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	dirs := isolateHome(t)
	seed(t, dirs.Config, "config.json", `{}`)
	seed(t, dirs.Data, "credentials.json", `{}`)
	seed(t, dirs.Cache, "models.json", `[]`)

	if err := a.runUninstall(context.Background(), []string{"--keep-data", "--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dirs.Config, "config.json"),
		filepath.Join(dirs.Data, "credentials.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("--keep-data removed %s", path)
		}
	}
	if _, err := os.Stat(dirs.Cache); err == nil {
		t.Error("--keep-data kept the rebuildable cache, which is not data")
	}
	if !strings.Contains(stdout.String(), "Kept:") {
		t.Errorf("what was kept was not reported:\n%s", stdout.String())
	}
}

// A directory that is not there must not be listed: a list of paths that do not
// exist reads as a threat to delete things that were never kolk's.
func TestUninstallOnlyListsPathsThatExist(t *testing.T) {
	a, stdout, _ := newTestApp(t, "n\n")
	dirs := isolateHome(t)
	seed(t, dirs.Config, "config.json", `{}`)

	if err := a.runUninstall(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); strings.Contains(out, dirs.Cache) {
		t.Errorf("a directory that does not exist was listed:\n%s", out)
	}
}

func TestUninstallRejectsUnknownFlags(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	if err := a.runUninstall(context.Background(), []string{"--purge"}); err == nil {
		t.Error("an unknown flag was accepted")
	}
}

func seed(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
