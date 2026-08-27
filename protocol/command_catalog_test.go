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

func TestCommandVocabularyIsClosedAcrossCodeSchemasAndGoldens(t *testing.T) {
	want := []CommandType{CommandTurnStart, CommandTurnCancel, CommandPermissionResolve}
	if got := KnownCommandTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("known command catalog = %v, want %v", got, want)
	}
	copyOfCatalog := KnownCommandTypes()
	copyOfCatalog[0] = CommandType("mutated.by.client")
	if got := KnownCommandTypes(); !reflect.DeepEqual(got, want) {
		t.Fatal("KnownCommandTypes returned mutable shared storage")
	}

	source := commandConstantsFromSource(t)
	assertUniqueCommandSet(t, "Go command constants", source)
	assertUniqueCommandSet(t, "known command catalog", want)
	assertSameCommandSet(t, "Go command constants", source, "known command catalog", want)

	schemas := commandFiles(t, filepath.Join("..", "spec", "schemas", "commands"))
	goldens := commandFiles(t, filepath.Join("..", "spec", "testdata", "commands"))
	assertSameCommandSet(t, "known command catalog", want, "command schemas", schemas)
	assertSameCommandSet(t, "known command catalog", want, "command goldens", goldens)

	for _, command := range want {
		t.Run(string(command), func(t *testing.T) {
			assertCatalogCommandSchemaID(t, command)
			assertCatalogCommandGolden(t, command)
		})
	}
}

func commandConstantsFromSource(t *testing.T) []CommandType {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "command.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var commands []CommandType
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
				if !name.IsExported() || !strings.HasPrefix(name.Name, "Command") {
					continue
				}
				if _, duplicate := names[name.Name]; duplicate {
					t.Fatalf("duplicate exported command constant name %s", name.Name)
				}
				names[name.Name] = struct{}{}
				typed, ok := spec.Type.(*ast.Ident)
				if !ok || typed.Name != "CommandType" || len(spec.Values) != len(spec.Names) {
					t.Fatalf("%s must be an explicit CommandType string constant", name.Name)
				}
				literal, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s must use a string literal", name.Name)
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s has invalid string literal: %v", name.Name, err)
				}
				commands = append(commands, CommandType(value))
			}
		}
	}
	return commands
}

func commandFiles(t *testing.T, dir string) []CommandType {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]CommandType, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			t.Fatalf("unexpected non-JSON command contract entry %s", filepath.Join(dir, entry.Name()))
		}
		commands = append(commands, CommandType(strings.TrimSuffix(entry.Name(), ".json")))
	}
	return commands
}

func assertUniqueCommandSet(t *testing.T, label string, commands []CommandType) {
	t.Helper()
	seen := make(map[CommandType]struct{}, len(commands))
	for _, command := range commands {
		if _, duplicate := seen[command]; duplicate {
			t.Errorf("%s contains duplicate wire value %q", label, command)
		}
		seen[command] = struct{}{}
	}
}

func assertSameCommandSet(
	t *testing.T,
	leftLabel string,
	left []CommandType,
	rightLabel string,
	right []CommandType,
) {
	t.Helper()
	leftCopy := append([]CommandType(nil), left...)
	rightCopy := append([]CommandType(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i] < leftCopy[j] })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i] < rightCopy[j] })
	if !reflect.DeepEqual(leftCopy, rightCopy) {
		t.Errorf("%s = %v, %s = %v", leftLabel, leftCopy, rightLabel, rightCopy)
	}
}

func assertCatalogCommandSchemaID(t *testing.T, command CommandType) {
	t.Helper()
	path := filepath.Join("..", "spec", "schemas", "commands", string(command)+".json")
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
	want := "https://kolkrabbi.francomichetti.com/spec/0/schemas/commands/" + string(command) + ".json"
	if schema.ID != want {
		t.Errorf("schema id = %q, want %q", schema.ID, want)
	}
}

func assertCatalogCommandGolden(t *testing.T, command CommandType) {
	t.Helper()
	path := filepath.Join("..", "spec", "testdata", "commands", string(command)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
		t.Fatalf("%s is not one JSON object", path)
	}
	var errValidation error
	switch command {
	case CommandTurnStart:
		errValidation = validateTurnStartCommand(raw)
	case CommandTurnCancel:
		errValidation = validateTurnCancelCommand(raw)
	case CommandPermissionResolve:
		errValidation = validatePermissionResolveCommand(raw)
	default:
		t.Fatalf("catalog command %q has no conformance validator", command)
	}
	if errValidation != nil {
		t.Errorf("golden validation: %v", errValidation)
	}
}
