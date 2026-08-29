package engine

import (
	"fmt"
	"strings"
)

// ownedPrefixes are the model-id prefixes the engine routes itself rather than
// forwarding to the session backend. Every gateway id has the shape
// vendor/model, so a prefix is only a route when it is listed here: anything
// else is a vendor and passes through untouched.
//
// An owned prefix with no backend registered is refused, never forwarded. The
// gateway has never heard of the id, so at best it is a 404 about a model the
// user did not type — and at worst a gateway that happened to know the name
// would answer it for money.
var ownedPrefixes = map[string]string{
	"ollama": "a local Ollama server",
}

// backendFor resolves the backend that answers for a model id, and the id it
// should be asked for. The session backend answers for everything that is not
// an owned prefix; a routed id reaches its backend with the prefix stripped,
// because the server behind it has never seen the prefix.
//
// Routes are separate from a.Backend on purpose: moveToMetered and switchModel
// swap a.Backend between the plan provider and the gateway client, and a route
// that lived there would be lost on the first swap.
func (a *Agent) backendFor(model string) (ChatBackend, string, error) {
	prefix, rest, found := strings.Cut(model, "/")
	if !found {
		return a.Backend, model, nil
	}
	what, owned := ownedPrefixes[prefix]
	if !owned {
		return a.Backend, model, nil
	}
	if backend := a.Routes[prefix]; backend != nil {
		return backend, rest, nil
	}
	return nil, "", fmt.Errorf("%s needs %s and this session has none attached; `/model` lists what can answer", model, what)
}
