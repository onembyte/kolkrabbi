package tools

import "encoding/json"

// SchemaCost reports what the tool schemas cost one request: the total bytes
// sent, and the bytes each tool contributes.
//
// It exists because item 16's design work was about to proceed on a number
// nobody had taken — the doc said the built-in schemas were "about 5 KB" and
// they are 2,816 bytes. A search-and-load bridge for MCP tools is the right
// shape whatever the number, but the number decides how urgent it is, and
// guessing it high would have justified more mechanism than the problem needs.
//
// Anything that adds tools — MCP servers above all, since one can add a dozen
// at once — should be able to say what it costs before it is switched on.
func SchemaCost() (total int, perTool map[string]int) {
	defs := Definitions()
	perTool = make(map[string]int, len(defs))
	for _, def := range defs {
		if encoded, err := json.Marshal(def); err == nil {
			perTool[def.Function.Name] = len(encoded)
		}
	}
	if encoded, err := json.Marshal(defs); err == nil {
		total = len(encoded)
	}
	return total, perTool
}
