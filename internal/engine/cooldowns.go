package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Cooldown is one remembered limit: what it was, what it is keyed on, and
// when it lifts. It exists so that a capped key is skipped, not hammered, and
// so that a session -- or the next one -- can say when work resumes.
type Cooldown struct {
	Key       string              `json:"key"`
	Kind      provider.LimitKind  `json:"kind"`
	Scope     provider.LimitScope `json:"scope"`
	Connector string              `json:"connector,omitempty"`
	Model     string              `json:"model,omitempty"`
	Source    string              `json:"source,omitempty"`
	Message   string              `json:"message,omitempty"`
	Marked    time.Time           `json:"marked"`
	Until     time.Time           `json:"until"`
}

// Cooldowns is the registry (plan 35 §2.1). Two files: model- and endpoint-
// scope limits are the session's and live beside it; account-scope limits --
// a plan's window, an account out of credit -- are the user's and
// live at the data directory, where every session reads them. Writes go
// through the atomic writer; reads fail closed to "nothing remembered".
type Cooldowns struct {
	mu          sync.Mutex
	sessionFile string
	sharedFile  string
	now         func() time.Time
	entries     map[string]Cooldown
}

// OpenCooldowns loads both files. A file that does not exist, or cannot be
// read, is an empty registry: a limit forgotten is a limit met again, which is
// the failure mode this registry exists to reduce, never one it can worsen.
func OpenCooldowns(sessionFile, sharedFile string) *Cooldowns {
	return openCooldownsAt(sessionFile, sharedFile, time.Now)
}

// openCooldownsAt is OpenCooldowns with the clock injected, so a test can load
// and expire entries against a fixed time rather than the wall.
func openCooldownsAt(sessionFile, sharedFile string, now func() time.Time) *Cooldowns {
	c := &Cooldowns{sessionFile: sessionFile, sharedFile: sharedFile, now: now, entries: map[string]Cooldown{}}
	_ = c.Reload()
	return c
}

// Reload re-reads both files, so a running session learns of a limit another
// session hit. Entries already lifted are dropped on the way in.
func (c *Cooldowns) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh := map[string]Cooldown{}
	var firstErr error
	for _, path := range []string{c.sharedFile, c.sessionFile} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		var list []Cooldown
		if err := json.Unmarshal(data, &list); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, cd := range list {
			if cd.Until.After(c.now()) {
				fresh[cd.Key] = cd
			}
		}
	}
	c.entries = fresh
	return firstErr
}

// Mark records a classified limit and returns the cooldown it became. A limit
// with a reset lifts then; one with a Retry-After lifts after it; one with
// neither holds for its kind's default. A kind with no default -- a budget
// stop -- is the user's to lift and is not recorded.
func (c *Cooldowns) Mark(limit provider.Limit) (Cooldown, bool) {
	if c == nil {
		return Cooldown{}, false
	}
	now := c.now()
	until := limit.ResetAt
	switch {
	case !until.IsZero():
	case limit.RetryAfter > 0:
		until = now.Add(limit.RetryAfter)
	case limit.Kind.DefaultCooldown() > 0:
		until = now.Add(limit.Kind.DefaultCooldown())
	default:
		return Cooldown{}, false
	}
	cd := Cooldown{
		Key: cooldownKey(limit.Scope, limit.Connector, limit.Model), Kind: limit.Kind, Scope: limit.Scope,
		Connector: limit.Connector, Model: limit.Model, Source: limit.Source, Message: limit.Message,
		Marked: now, Until: until,
	}
	c.mu.Lock()
	c.entries[cd.Key] = cd
	err := c.saveLocked()
	c.mu.Unlock()
	_ = err // a registry that cannot write still remembers for this process
	return cd, true
}

// Cooling reports whether the key for this scope is still cooling, and until
// when. Lifted entries are pruned as they are met.
func (c *Cooldowns) Cooling(scope provider.LimitScope, connector, model string) (Cooldown, bool) {
	if c == nil {
		return Cooldown{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cooldownKey(scope, connector, model)
	cd, ok := c.entries[key]
	if !ok {
		return Cooldown{}, false
	}
	if !cd.Until.After(c.now()) {
		delete(c.entries, key)
		return Cooldown{}, false
	}
	return cd, true
}

// Active lists what is still cooling, soonest to lift first, for /doctor and
// the status line. Lifted entries are pruned on the way.
func (c *Cooldowns) Active() []Cooldown {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	out := make([]Cooldown, 0, len(c.entries))
	for key, cd := range c.entries {
		if cd.Until.After(now) {
			out = append(out, cd)
		} else {
			delete(c.entries, key)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until.Before(out[j].Until) })
	return out
}

// Describe is the one line a cooldown is shown as: what is cooling, why, and
// when it lifts, in the local clock the person reading it keeps.
func (cd Cooldown) Describe() string {
	what := cd.Connector
	if cd.Scope == provider.ScopeModel && cd.Model != "" {
		what = cd.Model
	}
	return fmt.Sprintf("%s · %s · resumes %s", what, strings.ReplaceAll(string(cd.Kind), "_", " "), cd.Until.Local().Format("15:04"))
}

// shared says which file a scope belongs to: the user's, or the session's.
func shared(scope provider.LimitScope) bool {
	return scope == provider.ScopeAccount
}

func cooldownKey(scope provider.LimitScope, connector, model string) string {
	key := string(scope) + "|" + connector
	if scope == provider.ScopeModel {
		key += "|" + model
	}
	return key
}

// saveLocked writes each file with the entries that belong to it. The shared
// file is merged with what other sessions have written since the last reload,
// so one session's write does not erase another's limit.
func (c *Cooldowns) saveLocked() error {
	var session, user []Cooldown
	for _, cd := range c.entries {
		if shared(cd.Scope) {
			user = append(user, cd)
		} else {
			session = append(session, cd)
		}
	}
	var firstErr error
	if c.sessionFile != "" {
		if err := writeCooldowns(c.sessionFile, session); err != nil {
			firstErr = err
		}
	}
	if c.sharedFile != "" {
		merged := mergeOthers(c.sharedFile, user, c.now())
		if err := writeCooldowns(c.sharedFile, merged); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// mergeOthers keeps other sessions' still-cooling entries that this registry
// does not know, so a write from here never discards a limit met elsewhere.
func mergeOthers(path string, ours []Cooldown, now time.Time) []Cooldown {
	known := map[string]bool{}
	for _, cd := range ours {
		known[cd.Key] = true
	}
	out := append([]Cooldown(nil), ours...)
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var theirs []Cooldown
	if json.Unmarshal(data, &theirs) != nil {
		return out
	}
	for _, cd := range theirs {
		if !known[cd.Key] && cd.Until.After(now) {
			out = append(out, cd)
		}
	}
	return out
}

func writeCooldowns(path string, list []Cooldown) error {
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	data, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

// CoolingNotice is what the status line says while the session's own connector
// or model is cooling: one line, or nothing at all when nothing is.
func (a *Agent) CoolingNotice() string {
	if a.Cooldowns == nil {
		return ""
	}
	model := a.SessionModel()
	connector := a.connectorFor(model)
	if cd, ok := a.Cooldowns.Cooling(provider.ScopeAccount, connector, ""); ok {
		return "cooling · " + cd.Describe()
	}
	if cd, ok := a.Cooldowns.Cooling(provider.ScopeModel, connector, model); ok {
		return "cooling · " + cd.Describe()
	}
	if cd, ok := a.Cooldowns.Cooling(provider.ScopeEndpoint, connector, ""); ok {
		return "cooling · " + cd.Describe()
	}
	return ""
}
