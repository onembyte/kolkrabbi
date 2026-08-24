package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type openAPIDocument struct {
	OpenAPI           string                                `json:"openapi"`
	JSONSchemaDialect string                                `json:"jsonSchemaDialect"`
	Info              openAPIInfo                           `json:"info"`
	Protocol          string                                `json:"x-kolk-protocol"`
	Security          []map[string][]string                 `json:"security"`
	Paths             map[string]map[string]json.RawMessage `json:"paths"`
	Components        openAPIComponents                     `json:"components"`
}

type openAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type openAPIComponents struct {
	SecuritySchemes map[string]openAPISecurityScheme `json:"securitySchemes"`
	Schemas         map[string]json.RawMessage       `json:"schemas"`
	Responses       map[string]openAPIResponse       `json:"responses"`
}

type openAPISecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
}

type openAPIOperation struct {
	OperationID string                     `json:"operationId"`
	Command     CommandType                `json:"x-kolk-command"`
	Event       EventType                  `json:"x-kolk-event"`
	Security    json.RawMessage            `json:"security"`
	Parameters  []openAPIParameter         `json:"parameters"`
	RequestBody *openAPIRequestBody        `json:"requestBody"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIParameter struct {
	Name     string          `json:"name"`
	In       string          `json:"in"`
	Required bool            `json:"required"`
	Schema   json.RawMessage `json:"schema"`
}

type openAPIRequestBody struct {
	Required bool                    `json:"required"`
	Content  map[string]openAPIMedia `json:"content"`
}

type openAPIResponse struct {
	Ref         string                  `json:"$ref"`
	Description string                  `json:"description"`
	Content     map[string]openAPIMedia `json:"content"`
}

type openAPIMedia struct {
	Schema json.RawMessage `json:"schema"`
}

func TestOpenAPIContainsOnlyOwnerStableOperations(t *testing.T) {
	document, raw := readOpenAPIDocument(t)

	if document.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", document.OpenAPI)
	}
	if document.JSONSchemaDialect != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("jsonSchemaDialect = %q", document.JSONSchemaDialect)
	}
	if document.Info.Title != "Kolkrabbi Protocol" || document.Info.Version != Version {
		t.Errorf("info = %#v, want title and protocol version %q", document.Info, Version)
	}
	if document.Protocol != Version {
		t.Errorf("x-kolk-protocol = %q, want %q", document.Protocol, Version)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		t.Error("OpenAPI contract must use JSON syntax, which is also valid YAML 1.2")
	}

	wantPaths := map[string][]string{
		"/v1/hello":             {"get"},
		"/v1/permissions/{id}":  {"post"},
		"/v1/turns/{id}/cancel": {"post"},
	}
	if got := pathMethodInventory(document.Paths); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("path/method inventory = %#v, want %#v", got, wantPaths)
	}
	for path := range document.Paths {
		lower := strings.ToLower(path)
		for _, forbidden := range []string{"credential", "key", "login", "auth"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("secret-management path leaked into protocol contract: %s", path)
			}
		}
	}
}

func TestOpenAPISecurityAndSharedResponses(t *testing.T) {
	document, _ := readOpenAPIDocument(t)
	wantSecurity := []map[string][]string{{"bearerAuth": {}}}
	if !reflect.DeepEqual(document.Security, wantSecurity) {
		t.Errorf("global security = %#v, want %#v", document.Security, wantSecurity)
	}
	if !reflect.DeepEqual(document.Components.SecuritySchemes, map[string]openAPISecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer"},
	}) {
		t.Errorf("security schemes = %#v", document.Components.SecuritySchemes)
	}

	hello := operationAt(t, document, "/v1/hello", "get")
	if string(hello.Security) != "[]" {
		t.Errorf("hello security = %s, want explicit empty override", hello.Security)
	}
	for _, target := range []struct{ path, method string }{
		{"/v1/turns/{id}/cancel", "post"},
		{"/v1/permissions/{id}", "post"},
	} {
		operation := operationAt(t, document, target.path, target.method)
		if operation.Security != nil {
			t.Errorf("%s %s overrides global bearer security: %s", target.method, target.path, operation.Security)
		}
	}

	assertSchemaRef(t, document.Components.Schemas["Hello"], "./schemas/events/hello.json")
	assertSchemaRef(t, document.Components.Schemas["Error"], "./schemas/entities/error.json")
	assertResponseRef(t, hello.Responses["default"], "#/components/responses/ErrorResponse")
	helloOK := hello.Responses["200"]
	if helloOK.Description == "" || len(helloOK.Content) != 1 {
		t.Fatalf("hello 200 response = %#v", helloOK)
	}
	assertSchemaRef(t, helloOK.Content["application/json"].Schema, "#/components/schemas/Hello")

	errorResponse, ok := document.Components.Responses["ErrorResponse"]
	if !ok || errorResponse.Description == "" || len(errorResponse.Content) != 1 {
		t.Fatalf("shared error response = %#v, present %t", errorResponse, ok)
	}
	assertSchemaRef(t, errorResponse.Content["application/json"].Schema, "#/components/schemas/Error")
	assertExternalSchemaRefsResolve(t, rawOpenAPIValue(t))
}

func TestOpenAPIMutationsAreDerivedFromShippedCommands(t *testing.T) {
	document, _ := readOpenAPIDocument(t)

	turnSchema := readJSONMap(t, filepath.Join("..", "spec", "schemas", "commands", "turn.cancel.json"))
	permissionSchema := readJSONMap(t, filepath.Join("..", "spec", "schemas", "commands", "permission.resolve.json"))
	turnID := schemaProperty(t, turnSchema, "turn_id")
	permissionID := schemaProperty(t, permissionSchema, "id")
	decision := schemaProperty(t, permissionSchema, "decision")

	cancel := operationAt(t, document, "/v1/turns/{id}/cancel", "post")
	if cancel.OperationID != "cancelTurn" || cancel.Command != CommandTurnCancel {
		t.Errorf("cancel operation identity = (%q, %q)", cancel.OperationID, cancel.Command)
	}
	assertOnePathParameter(t, cancel.Parameters, "id", turnID)
	if cancel.RequestBody != nil {
		t.Error("turn cancellation must not make clients repeat the path ID in a request body")
	}
	assertNoContentMutationResponses(t, cancel.Responses)

	resolve := operationAt(t, document, "/v1/permissions/{id}", "post")
	if resolve.OperationID != "resolvePermission" || resolve.Command != CommandPermissionResolve {
		t.Errorf("permission operation identity = (%q, %q)", resolve.OperationID, resolve.Command)
	}
	assertOnePathParameter(t, resolve.Parameters, "id", permissionID)
	if resolve.RequestBody == nil || !resolve.RequestBody.Required || len(resolve.RequestBody.Content) != 1 {
		t.Fatalf("permission request body = %#v", resolve.RequestBody)
	}
	assertSchemaRef(t, resolve.RequestBody.Content["application/json"].Schema, "#/components/schemas/PermissionDecisionInput")
	assertNoContentMutationResponses(t, resolve.Responses)

	input := decodeRawMap(t, document.Components.Schemas["PermissionDecisionInput"])
	if input["type"] != "object" || input["additionalProperties"] != true {
		t.Errorf("permission decision input object flags = %#v", input)
	}
	if !reflect.DeepEqual(input["required"], []any{"decision"}) {
		t.Errorf("permission decision required = %#v", input["required"])
	}
	properties, ok := input["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		t.Fatalf("permission decision properties = %#v", input["properties"])
	}
	if !reflect.DeepEqual(properties["decision"], decision) {
		t.Errorf("OpenAPI decision = %#v, command decision = %#v", properties["decision"], decision)
	}

	var commands []CommandType
	for path, methods := range document.Paths {
		for method := range methods {
			operation := operationAt(t, document, path, method)
			if operation.Command != "" {
				commands = append(commands, operation.Command)
			}
		}
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i] < commands[j] })
	want := KnownCommandTypes()
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(commands, want) {
		t.Errorf("OpenAPI command operations = %v, shipped commands = %v", commands, want)
	}

	hello := operationAt(t, document, "/v1/hello", "get")
	if hello.OperationID != "getHello" || hello.Event != EventHello || hello.Command != "" {
		t.Errorf("hello operation identity = (%q, event %q, command %q)", hello.OperationID, hello.Event, hello.Command)
	}
}

func readOpenAPIDocument(t *testing.T) (openAPIDocument, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "kolk.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var document openAPIDocument
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("OpenAPI document must be JSON syntax valid as YAML 1.2: %v", err)
	}
	if decoder.Decode(&struct{}{}) == nil {
		t.Fatal("OpenAPI document contains a trailing JSON value")
	}
	return document, raw
}

func rawOpenAPIValue(t *testing.T) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "kolk.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func pathMethodInventory(paths map[string]map[string]json.RawMessage) map[string][]string {
	inventory := make(map[string][]string, len(paths))
	for path, methods := range paths {
		for method := range methods {
			inventory[path] = append(inventory[path], method)
		}
		sort.Strings(inventory[path])
	}
	return inventory
}

func operationAt(t *testing.T, document openAPIDocument, path, method string) openAPIOperation {
	t.Helper()
	raw, ok := document.Paths[path][method]
	if !ok {
		t.Fatalf("missing operation %s %s", method, path)
	}
	var operation openAPIOperation
	if err := json.Unmarshal(raw, &operation); err != nil {
		t.Fatalf("decode operation %s %s: %v", method, path, err)
	}
	return operation
}

func assertOnePathParameter(t *testing.T, parameters []openAPIParameter, name string, wantSchema any) {
	t.Helper()
	if len(parameters) != 1 {
		t.Fatalf("path parameters = %#v, want one", parameters)
	}
	parameter := parameters[0]
	if parameter.Name != name || parameter.In != "path" || !parameter.Required {
		t.Errorf("path parameter = %#v", parameter)
	}
	if got := decodeRaw(t, parameter.Schema); !reflect.DeepEqual(got, wantSchema) {
		t.Errorf("path parameter schema = %#v, command schema = %#v", got, wantSchema)
	}
}

func assertNoContentMutationResponses(t *testing.T, responses map[string]openAPIResponse) {
	t.Helper()
	if len(responses) != 2 {
		t.Fatalf("mutation responses = %#v, want only 204 and default", responses)
	}
	noContent, ok := responses["204"]
	if !ok || noContent.Description == "" || noContent.Ref != "" || len(noContent.Content) != 0 {
		t.Errorf("204 response = %#v, present %t", noContent, ok)
	}
	assertResponseRef(t, responses["default"], "#/components/responses/ErrorResponse")
}

func assertResponseRef(t *testing.T, response openAPIResponse, want string) {
	t.Helper()
	if response.Ref != want || response.Description != "" || len(response.Content) != 0 {
		t.Errorf("response reference = %#v, want only $ref %q", response, want)
	}
}

func assertSchemaRef(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	got := decodeRawMap(t, raw)
	if !reflect.DeepEqual(got, map[string]any{"$ref": want}) {
		t.Errorf("schema = %#v, want only $ref %q", got, want)
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func schemaProperty(t *testing.T, schema map[string]any, name string) any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", schema["properties"])
	}
	property, ok := properties[name]
	if !ok {
		t.Fatalf("schema has no property %q", name)
	}
	return property
}

func decodeRawMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	value := decodeRaw(t, raw)
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("JSON value = %#v, want object", value)
	}
	return object
}

func decodeRaw(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("missing JSON value")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertExternalSchemaRefsResolve(t *testing.T, value any) {
	t.Helper()
	var walk func(any)
	walk = func(node any) {
		switch node := node.(type) {
		case map[string]any:
			for key, child := range node {
				if key == "$ref" {
					ref, ok := child.(string)
					if !ok {
						t.Errorf("non-string $ref: %#v", child)
						continue
					}
					if strings.HasPrefix(ref, "./") {
						clean := filepath.Clean(filepath.Join("..", "spec", ref))
						specRoot := filepath.Clean(filepath.Join("..", "spec")) + string(filepath.Separator)
						if !strings.HasPrefix(clean, specRoot) {
							t.Errorf("external schema reference escapes spec: %q", ref)
						} else if _, err := os.Stat(clean); err != nil {
							t.Errorf("external schema reference %q: %v", ref, err)
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(value)
}
