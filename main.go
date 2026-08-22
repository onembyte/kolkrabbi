// Kolkrabbi (binary: kolk) is a fast, lightweight agentic CLI for any model
// on OpenRouter (or any OpenAI-compatible endpoint): three modes — chat,
// code (Claude-Code style tool loop), and agent (orchestrated
// plan/delegate/synthesize) — with an effort dial that scales model tier and
// orchestration depth, persistent sessions, rewindable file checkpoints, and
// a 100% local usage/rating dashboard.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kolkrabbi/internal/agent"
	"kolkrabbi/internal/api"
	"kolkrabbi/internal/checkpoint"
	"kolkrabbi/internal/config"
	"kolkrabbi/internal/session"
	"kolkrabbi/internal/stats"
)

const defaultModel = "openrouter/auto" // OpenRouter's auto-router; override with -m or `kolk config set-model`

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kolk"
	}
	return filepath.Join(home, ".config", "kolk")
}

func sessionsDir() string { return filepath.Join(configDir(), "sessions") }

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "config":
			runConfigCmd(args[1:])
			return
		case "models":
			runModelsCmd(args[1:])
			return
		case "sessions":
			runSessionsCmd(args[1:])
			return
		case "stats":
			runStatsCmd(args[1:])
			return
		case "-h", "--help", "help":
			printUsage()
			return
		}
	}

	var model, prompt, sessID, baseURL string
	mode, effort := "", ""
	yolo, resume := false, false

	rest := args[:0:0]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m", "--model":
			i++
			if i < len(args) {
				model = args[i]
			}
		case "--mode":
			i++
			if i < len(args) {
				mode = args[i]
			}
		case "-e", "--effort":
			i++
			if i < len(args) {
				effort = args[i]
			}
		case "-y", "--yolo":
			yolo = true
		case "-r", "--resume":
			resume = true
		case "-s", "--session":
			i++
			if i < len(args) {
				sessID = args[i]
			}
		case "--base-url":
			i++
			if i < len(args) {
				baseURL = args[i]
			}
		case "-p", "--print":
			i++
			if i < len(args) {
				prompt = args[i]
			}
		default:
			rest = append(rest, args[i])
		}
	}
	if prompt == "" && len(rest) > 0 {
		prompt = strings.Join(rest, " ")
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	apiKey := config.ResolveAPIKey(cfg)
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "No OpenRouter API key found.")
		fmt.Fprintln(os.Stderr, "  Set one with:   kolk config set-key sk-or-v1-...")
		fmt.Fprintln(os.Stderr, "  or:             export OPENROUTER_API_KEY=sk-or-v1-...")
		os.Exit(1)
	}

	// pick or create the session
	sdir := sessionsDir()
	var sess *session.Session
	switch {
	case sessID != "":
		sess, err = session.Load(sdir, sessID)
		if err != nil {
			fatal(fmt.Errorf("cannot load session %s: %w (try `kolk sessions`)", sessID, err))
		}
	case resume:
		sess, err = session.Latest(sdir)
		if err != nil {
			fatal(err)
		}
		if sess == nil {
			fmt.Println("no previous session found, starting a new one.")
		}
	}
	if sess == nil {
		sess = session.New(sdir, "")
	}

	// model precedence: -m flag > resumed session's model > config > default
	if model == "" {
		model = sess.Model
	}
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = defaultModel
	}
	sess.Model = model

	client := api.NewClient(apiKey)
	client.BaseURL = resolveBaseURL(baseURL, cfg)

	ckpt, err := checkpoint.Open(sess.CkptDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: checkpoints disabled: %v\n", err)
		ckpt = nil
	}

	stdin := bufio.NewReader(os.Stdin)
	ag := agent.New(agent.Options{
		Client:   client,
		Model:    model,
		Mode:     mode,
		Effort:   effort,
		Yolo:     yolo,
		Sess:     sess,
		Ckpt:     ckpt,
		In:       stdin,
		Out:      os.Stdout,
		StatsDir: configDir(),
		Tiers:    cfg.Tiers,
	})

	if prompt != "" {
		// single-shot mode: run one turn and exit
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := ag.RunTurn(ctx, prompt); err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "(interrupted)")
				os.Exit(130)
			}
			fatal(err)
		}
		return
	}

	runREPL(ag, stdin)
}

func resolveBaseURL(flagVal string, cfg *config.Config) string {
	if flagVal != "" {
		return strings.TrimRight(flagVal, "/")
	}
	if v := os.Getenv("OPENROUTER_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return api.DefaultBaseURL
}

func runREPL(ag *agent.Agent, reader *bufio.Reader) {
	resumedNote := ""
	if n := len(ag.Sess.Messages); n > 1 {
		resumedNote = fmt.Sprintf("  (resumed, %d messages)", n-1)
	}
	fmt.Printf("kolk — mode: %s · effort: %s · model: %s%s\nsession: %s%s\n",
		ag.Mode, ag.Effort, ag.Model, yoloTag(ag.Yolo), ag.Sess.ID, resumedNote)
	fmt.Println("Type your request, or /help for commands. Ctrl+C interrupts a turn, /exit quits.")

	for {
		fmt.Printf("\n\033[1m%s>\033[0m ", ag.Mode)
		line, err := reader.ReadString('\n')
		if err != nil {
			return // EOF (Ctrl+D)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if handleSlash(ag, line) {
				return
			}
			continue
		}

		// per-turn interrupt: Ctrl+C cancels this turn only, not the REPL
		tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		err = ag.RunTurn(tctx, line)
		stop()
		if errors.Is(err, context.Canceled) {
			fmt.Println("\033[2m(interrupted)\033[0m")
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31merror:\033[0m %v\n", err)
		}
	}
}

// handleSlash processes a /command; returns true if the REPL should exit.
func handleSlash(ag *agent.Agent, line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, cmd))

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		fmt.Print(`/mode <chat|code|agent>   switch mode (agent = orchestrated)
/effort <quick|standard|deep|ultra>   scale model tier + orchestration depth
/model <id>    override the base model for this session
/rate <1-5>    rate the last turn (feeds the local dashboard)
/yolo          toggle auto-approve of tool actions
/new           start a fresh session (current one stays saved)
/session       show current session id and file
/changes       list files this session has modified
/rewind        undo the last turn's file changes (repeatable, files only)
/exit          quit
`)
	case "/mode":
		if arg == "" {
			fmt.Printf("mode: %s (chat|code|agent)\n", ag.Mode)
			break
		}
		if err := ag.SetMode(arg); err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("mode: %s\n", ag.Mode)
		}
	case "/effort":
		if arg == "" {
			fmt.Printf("effort: %s (quick|standard|deep|ultra)\n", ag.Effort)
			break
		}
		if err := ag.SetEffort(arg); err != nil {
			fmt.Println(err)
		} else {
			m := ag.Model
			if t, ok := ag.Tiers[ag.Effort]; ok && t != "" {
				m = t
			}
			fmt.Printf("effort: %s → %s\n", ag.Effort, m)
		}
	case "/rate":
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Println("usage: /rate <1-5>")
			break
		}
		if err := ag.RateLast(n); err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("rated %d★ — see `kolk stats`\n", n)
		}
	case "/new", "/clear":
		sess := session.New(sessionsDir(), ag.Model)
		ckpt, err := checkpoint.Open(sess.CkptDir())
		if err != nil {
			ckpt = nil
		}
		opts := ag.Options
		opts.Sess = sess
		opts.Ckpt = ckpt
		*ag = *agent.New(opts)
		fmt.Printf("new session: %s\n", sess.ID)
	case "/session":
		fmt.Printf("id:    %s\nfile:  %s\n", ag.Sess.ID, filepath.Join(sessionsDir(), ag.Sess.ID+".json"))
	case "/changes":
		if ag.Ckpt == nil {
			fmt.Println("checkpointing is not enabled.")
			break
		}
		ch := ag.Ckpt.Changes()
		if len(ch) == 0 {
			fmt.Println("no file changes recorded this session.")
			break
		}
		for _, e := range ch {
			verb := "edited"
			if !e.Existed {
				verb = "created"
			}
			fmt.Printf("turn %-3d %-8s %s (%s)\n", e.Turn, verb, e.Path, e.Tool)
		}
	case "/rewind":
		restored, err := ag.Rewind()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rewind failed: %v\n", err)
			break
		}
		if restored == nil {
			fmt.Println("nothing to rewind.")
			break
		}
		fmt.Println("restored:")
		for _, p := range restored {
			fmt.Println("  " + p)
		}
		fmt.Println("\033[2mnote: files only — the conversation history is unchanged.\033[0m")
	case "/yolo":
		ag.Yolo = !ag.Yolo
		fmt.Printf("yolo mode: %v\n", ag.Yolo)
	case "/model":
		if arg == "" {
			fmt.Println("usage: /model <model-id>")
		} else {
			ag.Model = arg
			ag.Sess.Model = arg
			fmt.Printf("model set to %s\n", arg)
		}
	default:
		fmt.Println("unknown command, /help for a list")
	}
	return false
}

func yoloTag(yolo bool) string {
	if yolo {
		return "  (yolo: auto-approving tool actions)"
	}
	return ""
}

func runConfigCmd(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	usage := "usage: kolk config [set-key <key> | set-model <model> | set-base-url <url> | set-tier <effort> <model> | show]"
	if len(args) == 0 {
		fmt.Println(usage)
		return
	}
	switch args[0] {
	case "set-key":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: kolk config set-key <key>"))
		}
		cfg.APIKey = args[1]
		saveCfg(cfg)
		fmt.Println("API key saved to ~/.config/kolk/config.json")
	case "set-model":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: kolk config set-model <model>"))
		}
		cfg.Model = strings.Join(args[1:], " ")
		saveCfg(cfg)
		fmt.Printf("default model set to %s\n", cfg.Model)
	case "set-base-url":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: kolk config set-base-url <url>"))
		}
		cfg.BaseURL = strings.TrimRight(args[1], "/")
		saveCfg(cfg)
		fmt.Printf("base URL set to %s\n", cfg.BaseURL)
	case "set-tier":
		if len(args) < 3 {
			fatal(fmt.Errorf("usage: kolk config set-tier <quick|standard|deep|ultra> <model>"))
		}
		valid := false
		for _, e := range agent.Efforts {
			if e == args[1] {
				valid = true
			}
		}
		if !valid {
			fatal(fmt.Errorf("unknown effort %q (quick|standard|deep|ultra)", args[1]))
		}
		if cfg.Tiers == nil {
			cfg.Tiers = map[string]string{}
		}
		cfg.Tiers[args[1]] = args[2]
		saveCfg(cfg)
		fmt.Printf("tier %s → %s\n", args[1], args[2])
	case "show":
		key := "(not set)"
		if cfg.APIKey != "" {
			key = maskKey(cfg.APIKey)
		}
		fmt.Printf("api_key:  %s\nmodel:    %s\nbase_url: %s\n",
			key,
			orDefault(cfg.Model, defaultModel+" (default)"),
			orDefault(cfg.BaseURL, api.DefaultBaseURL+" (default)"))
		if len(cfg.Tiers) > 0 {
			fmt.Println("tiers:")
			for _, e := range agent.Efforts {
				if m, ok := cfg.Tiers[e]; ok {
					fmt.Printf("  %-9s %s\n", e, m)
				}
			}
		} else {
			fmt.Println("tiers:    (none — all efforts use the session model; set with `kolk config set-tier`)")
		}
	default:
		fmt.Println(usage)
	}
}

func saveCfg(cfg *config.Config) {
	if err := config.Save(cfg); err != nil {
		fatal(err)
	}
}

func runStatsCmd(args []string) {
	recs, err := stats.Load(configDir())
	if err != nil {
		fatal(err)
	}
	rows := stats.Aggregate(recs)
	if len(args) > 0 && args[0] == "--json" {
		printJSON(rows)
		return
	}
	fmt.Print(stats.Render(rows))
	fmt.Printf("\nlocal data: %s (delete it any time; nothing ever leaves this machine)\n",
		filepath.Join(configDir(), "stats.jsonl"))
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(b))
}

func runSessionsCmd(args []string) {
	sdir := sessionsDir()
	if len(args) > 0 {
		switch args[0] {
		case "rm":
			if len(args) < 2 {
				fatal(fmt.Errorf("usage: kolk sessions rm <id>"))
			}
			if err := session.Delete(sdir, args[1]); err != nil {
				fatal(err)
			}
			fmt.Printf("deleted session %s\n", args[1])
			return
		case "clear":
			if err := session.Clear(sdir); err != nil {
				fatal(err)
			}
			fmt.Println("all sessions deleted.")
			return
		default:
			fatal(fmt.Errorf("usage: kolk sessions [rm <id> | clear]"))
		}
	}
	all, err := session.List(sdir)
	if err != nil {
		fatal(err)
	}
	if len(all) == 0 {
		fmt.Println("no sessions yet.")
		return
	}
	for _, s := range all {
		fmt.Printf("%-22s %s  %-32s msgs:%-4d %s\n",
			s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.Model, len(s.Messages), s.Title)
	}
	fmt.Println("\nresume the latest with `kolk -r`, or a specific one with `kolk -s <id>`")
}

func runModelsCmd(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	client := api.NewClient(config.ResolveAPIKey(cfg))
	client.BaseURL = resolveBaseURL("", cfg)
	models, err := client.ListModels(context.Background())
	if err != nil {
		fatal(err)
	}
	filter := strings.ToLower(strings.Join(args, " "))
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	for _, m := range models {
		if filter != "" && !strings.Contains(strings.ToLower(m.ID), filter) && !strings.Contains(strings.ToLower(m.Name), filter) {
			continue
		}
		fmt.Printf("%-48s ctx %-9d %s\n", m.ID, m.ContextLength, formatPricing(m.Pricing.Prompt, m.Pricing.Completion))
	}
}

// formatPricing converts OpenRouter's per-token USD strings to $/1M tokens.
func formatPricing(promptPerTok, completionPerTok string) string {
	in, err1 := strconv.ParseFloat(promptPerTok, 64)
	out, err2 := strconv.ParseFloat(completionPerTok, 64)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("in %s / out %s per token", promptPerTok, completionPerTok)
	}
	if in == 0 && out == 0 {
		return "free"
	}
	return fmt.Sprintf("$%.2f in / $%.2f out per 1M tokens", in*1e6, out*1e6)
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:6] + "…" + k[len(k)-4:]
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func printUsage() {
	fmt.Println(`kolk — chat / code / agent in one CLI, any model, any provider

Usage:
  kolk                          interactive session (mode: code)
  kolk --mode chat              plain chat, no tools
  kolk --mode agent             orchestrated: plan → subagents → synthesis
  kolk -e ultra "..."           effort: quick | standard | deep | ultra
  kolk "do the thing"           single-shot: one turn, then exit
  kolk -m <model> ...           use a specific model for this run
  kolk -y                       yolo mode: auto-approve all tool actions
  kolk -r                       resume the most recent session
  kolk -s <id>                  resume a specific session
  kolk --base-url <url>         any OpenAI-compatible endpoint (Ollama, LiteLLM, vLLM, mock)
  kolk stats [--json]           100% local usage & rating dashboard
  kolk sessions [rm <id>|clear] list / delete saved sessions
  kolk models [filter]          list models with context size and $/1M pricing
  kolk config set-key <key>
  kolk config set-model <id>
  kolk config set-base-url <url>
  kolk config set-tier <effort> <model>    e.g. set-tier quick google/gemini-2.5-flash
  kolk config show

Env:
  OPENROUTER_API_KEY             overrides the saved config key
  OPENROUTER_BASE_URL            overrides the saved base URL

Effort tiers map effort levels to models (quick→cheap, deep→frontier).
Unset tiers fall back to the session model, so zero config always works.
In agent mode, effort also scales orchestration width (2–6 subagent tasks).

Project memory: KOLKRABBI.md or AGENTS.md in the working directory is added to
the system prompt (like CLAUDE.md in Claude Code).`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
