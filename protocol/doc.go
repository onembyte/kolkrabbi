// Package protocol is Go's view of Kolkrabbi's language-neutral wire contract.
//
// The files under spec/ are the source of truth. This package is deliberately
// standard-library-only so another Go process can speak the protocol without
// importing the CLI, engine, providers, or operating-system adapters.
//
// Version 0 is still evolving. Decoders accept unknown fields and syntactically
// valid unknown event types so newer producers remain readable by older
// clients. Callers decide whether to handle or ignore an event type.
package protocol
