package protocol

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestEventVocabularyIsClosedAcrossCodeSchemasAndGoldens(t *testing.T) {
	want := []EventType{
		EventHello,
		EventMessageDelta, EventMessageCompleted, EventReasoningDelta,
		EventToolRequested, EventToolStarted, EventToolOutput, EventToolFinished,
		EventPermissionRequested, EventPermissionResolved,
		EventSubagentStarted, EventSubagentFinished,
		EventWorkUpdated,
		EventUsageReported, EventScoreRecorded, EventCheckpointCreated, EventError, EventLog,
		EventSessionStarted, EventSessionUpdated, EventSessionEnded,
		EventTurnStarted, EventTurnFinished, EventTurnCancelled,
	}
	if got := KnownEventTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("known event catalog = %v, want %v", got, want)
	}
	copyOfCatalog := KnownEventTypes()
	copyOfCatalog[0] = EventType("mutated.by.client")
	if got := KnownEventTypes(); !reflect.DeepEqual(got, want) {
		t.Fatal("KnownEventTypes returned mutable shared storage")
	}

	source := eventConstantsFromSource(t)
	assertUniqueEventSet(t, "Go event constants", source)
	assertUniqueEventSet(t, "known event catalog", want)
	assertSameEventSet(t, "Go event constants", source, "known event catalog", want)

	schemas := eventFiles(t, filepath.Join("..", "spec", "schemas", "events"))
	goldens := eventFiles(t, filepath.Join("..", "spec", "testdata", "events"))
	assertSameEventSet(t, "known event catalog", want, "event schemas", schemas)
	assertSameEventSet(t, "known event catalog", want, "event goldens", goldens)

	for _, event := range want {
		t.Run(string(event), func(t *testing.T) {
			assertCatalogSchemaID(t, event)
			assertCatalogGoldenType(t, event)
		})
	}
}

func TestClosedEventVocabularyKeepsUnknownEventsForwardCompatible(t *testing.T) {
	unknown := []byte(`{"seq":999,"ts":"2026-08-23T22:50:05Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"future.added","data":{"kept":true}}`)
	got, err := Decode(unknown)
	if err != nil {
		t.Fatalf("Decode rejected syntactically valid unknown event: %v", err)
	}
	if got.Type != EventType("future.added") || !strings.Contains(string(got.Data), `"kept":true`) {
		t.Errorf("unknown event was not retained: %#v", got)
	}
}

func eventConstantsFromSource(t *testing.T) []EventType {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "events.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var events []EventType
	names := make(map[string]struct{})
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, item := range gen.Specs {
			spec, ok := item.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range spec.Names {
				if !name.IsExported() || !strings.HasPrefix(name.Name, "Event") {
					continue
				}
				if _, duplicate := names[name.Name]; duplicate {
					t.Fatalf("duplicate exported event constant name %s", name.Name)
				}
				names[name.Name] = struct{}{}
				typed, ok := spec.Type.(*ast.Ident)
				if !ok || typed.Name != "EventType" || len(spec.Values) != len(spec.Names) {
					t.Fatalf("%s must be an explicit EventType string constant", name.Name)
				}
				literal, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s must use a string literal", name.Name)
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s has invalid string literal: %v", name.Name, err)
				}
				events = append(events, EventType(value))
			}
		}
	}
	return events
}

func eventFiles(t *testing.T, dir string) []EventType {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	events := make([]EventType, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			t.Fatalf("unexpected non-JSON event contract entry %s", filepath.Join(dir, entry.Name()))
		}
		events = append(events, EventType(strings.TrimSuffix(entry.Name(), ".json")))
	}
	return events
}

func assertUniqueEventSet(t *testing.T, label string, events []EventType) {
	t.Helper()
	seen := make(map[EventType]struct{}, len(events))
	for _, event := range events {
		if _, duplicate := seen[event]; duplicate {
			t.Errorf("%s contains duplicate wire value %q", label, event)
		}
		seen[event] = struct{}{}
	}
}

func assertSameEventSet(t *testing.T, leftLabel string, left []EventType, rightLabel string, right []EventType) {
	t.Helper()
	leftCopy := append([]EventType(nil), left...)
	rightCopy := append([]EventType(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i] < leftCopy[j] })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i] < rightCopy[j] })
	if !reflect.DeepEqual(leftCopy, rightCopy) {
		t.Errorf("%s = %v, %s = %v", leftLabel, leftCopy, rightLabel, rightCopy)
	}
}

func assertCatalogSchemaID(t *testing.T, event EventType) {
	t.Helper()
	path := filepath.Join("..", "spec", "schemas", "events", string(event)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("%s is not JSON: %v", path, err)
	}
	want := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/" + string(event) + ".json"
	if schema.ID != want {
		t.Errorf("schema id = %q, want %q", schema.ID, want)
	}
}

func assertCatalogGoldenType(t *testing.T, event EventType) {
	t.Helper()
	path := filepath.Join("..", "spec", "testdata", "events", string(event)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Decode(bytes.TrimSpace(raw))
	if err != nil {
		t.Fatalf("Decode(%s): %v", path, err)
	}
	if envelope.Type != event {
		t.Errorf("golden type = %q, want filename type %q", envelope.Type, event)
	}
}
