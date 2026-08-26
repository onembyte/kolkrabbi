package cli

import (
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// options are the settings one kolk run takes from its command line. Every
// field is optional: an empty options must produce a working agent, because
// `kolk` with no arguments is the product's front door.
type options struct {
	model        string
	mode         string
	effort       string
	session      string
	baseURL      string
	prompt       string
	yolo         bool
	resume       bool
	outputFormat string
	rest         []string // positional words, joined into the prompt
}

// flagDef is one command-line flag. The table below is the single source for
// both parsing and `kolk help`, so a flag can never exist without being
// documented, and documentation can never drift from behaviour.
type flagDef struct {
	long    string // without the leading --
	short   string // without the leading -; "" if there is none
	arg     string // placeholder shown in help; "" means the flag is a boolean
	summary string
	set     func(o *options, v string)
}

var flagTable = []flagDef{
	{long: "model", short: "m", arg: "<id>", summary: "use a specific model for this run",
		set: func(o *options, v string) { o.model = provider.ResolveModelAlias(v) }},
	{long: "mode", arg: "<chat|code|agent>", summary: "chat = no tools · code = tool loop (default) · agent = orchestrated",
		set: func(o *options, v string) { o.mode = v }},
	{long: "effort", short: "e", arg: "<low|medium|high|max>", summary: "select model tier and agent orchestration width",
		set: func(o *options, v string) { o.effort = v }},
	{long: "print", short: "p", arg: "<prompt>", summary: "single-shot: run one turn, then exit",
		set: func(o *options, v string) { o.prompt = v }},
	{long: "output-format", arg: "<text|stream-json>", summary: "output format for single-shot runs (default text)",
		set: func(o *options, v string) { o.outputFormat = v }},
	{long: "session", short: "s", arg: "<id>", summary: "resume a specific session",
		set: func(o *options, v string) { o.session = v }},
	{long: "resume", short: "r", summary: "resume the most recent session",
		set: func(o *options, _ string) { o.resume = true }},
	{long: "yolo", short: "y", summary: "auto-approve every tool action for this run",
		set: func(o *options, _ string) { o.yolo = true }},
	{long: "base-url", arg: "<url>", summary: "any OpenAI-compatible endpoint (Ollama, LiteLLM, vLLM, mock)",
		set: func(o *options, v string) { o.baseURL = v }},
}

func lookupFlag(arg string) *flagDef {
	switch {
	case strings.HasPrefix(arg, "--"):
		name := strings.TrimPrefix(arg, "--")
		for i := range flagTable {
			if flagTable[i].long == name {
				return &flagTable[i]
			}
		}
	case strings.HasPrefix(arg, "-") && len(arg) > 1:
		name := strings.TrimPrefix(arg, "-")
		for i := range flagTable {
			if flagTable[i].short != "" && flagTable[i].short == name {
				return &flagTable[i]
			}
		}
	}
	return nil
}

// parseFlags turns a command line into options. Unknown flags and missing
// values are errors rather than silently ignored words: a typo'd -mdoel used
// to become part of the prompt and get billed to the wrong model.
func parseFlags(args []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// "--" ends flag parsing; everything after it is prompt text, which is
		// the only way to start a prompt with a dash.
		if arg == "--" {
			o.rest = append(o.rest, args[i+1:]...)
			break
		}

		// --long=value, so a value that starts with a dash can still be passed.
		if name, value, ok := strings.Cut(arg, "="); ok && strings.HasPrefix(arg, "--") {
			f := lookupFlag(name)
			if f == nil {
				return nil, usagef("unknown flag %q (try `kolk help`)", name)
			}
			if f.arg == "" {
				return nil, usagef("flag --%s takes no value", f.long)
			}
			f.set(o, value)
			continue
		}

		f := lookupFlag(arg)
		if f == nil {
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return nil, usagef("unknown flag %q (try `kolk help`)", arg)
			}
			o.rest = append(o.rest, arg)
			continue
		}
		if f.arg == "" {
			f.set(o, "")
			continue
		}
		i++
		if i >= len(args) {
			return nil, usagef("flag %s needs a value: %s %s", arg, arg, f.arg)
		}
		f.set(o, args[i])
	}

	if o.prompt == "" && len(o.rest) > 0 {
		o.prompt = strings.Join(o.rest, " ")
	}
	if o.mode != "" && !releaseMode(o.mode) {
		return nil, usagef("unknown mode %q (chat|code|agent)", o.mode)
	}
	return o, nil
}

func releaseMode(mode string) bool {
	for _, candidate := range engine.Modes {
		if mode == candidate {
			return true
		}
	}
	return false
}
