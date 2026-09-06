package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/dash"
	"github.com/onembyte/kolkrabbi/internal/devices"
	"github.com/onembyte/kolkrabbi/protocol"
)

// clientCookie carries a device's token to the client routes and nowhere
// else: HttpOnly, SameSite=Strict, scoped to /v1, Secure over TLS. The
// pairing form sets it once; it never appears in a URL (plan 26 §5, I26.7).
const clientCookie = "kolk_device"

// clientPrefix is the only path under which the cookie is honoured; the API
// keeps its bearer header.
const clientPrefix = "/v1/client"

const clientCSS = `:root{color-scheme:light dark}body{font:15px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;margin:0;display:flex;flex-direction:column;height:100vh}
header{padding:.6rem 1rem;border-bottom:1px solid rgba(128,128,128,.3);display:flex;gap:1rem;align-items:baseline}h1{font-size:1.1rem;margin:0}
.sub{opacity:.7;font-size:.85rem}iframe{flex:1;border:0;width:100%}form{display:flex;gap:.5rem;padding:.6rem 1rem;border-top:1px solid rgba(128,128,128,.3)}
textarea{flex:1;font:inherit;min-height:2.6rem;resize:vertical}button{font:inherit;padding:0 1rem}
.ev{margin:.15rem 0;white-space:pre-wrap}.ev-turn{opacity:.6}.ev-tool{color:#7c5cff}.ev-err{color:#b45309}.ev-delta{display:inline}`

// clientTier is the tier behind a request: the main token steers; a device
// has its own.
func clientTier(r *http.Request, token string, store *devices.Store) (devices.Tier, bool) {
	provided := clientToken(r)
	if provided == "" {
		return "", false
	}
	if token != "" && provided == token {
		return devices.TierSteer, true
	}
	if device, ok := deviceOf(store, provided); ok {
		return device.Tier, true
	}
	return "", false
}

// clientToken is the cookie's value, or the bearer if a client sent one.
func clientToken(r *http.Request) string {
	if c, err := r.Cookie(clientCookie); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// sameOrigin is the client's CSRF rule: a post must carry an Origin or
// Referer whose host is this server's. The cookie's SameSite=Strict is the
// first line; this is the second.
func sameOrigin(r *http.Request) bool {
	for _, header := range []string{"Origin", "Referer"} {
		if v := r.Header.Get(header); v != "" {
			u, err := url.Parse(v)
			return err == nil && strings.EqualFold(u.Host, r.Host)
		}
	}
	return false
}

func setClientCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: clientCookie, Value: token, Path: "/v1", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		MaxAge: int((90 * 24 * time.Hour).Seconds()),
	})
}

// clientPageHandler is GET /v1/client: the page, server-rendered, no script.
// The transcript is an iframe on the stream; a steer device gets the form.
func clientPageHandler(token string, store *devices.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tier, ok := clientTier(r, token, store)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var b strings.Builder
		b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
		b.WriteString(`<title>kolk</title><link rel="manifest" href="/v1/client/manifest.json"><style>` + clientCSS + `</style></head><body>`)
		b.WriteString(`<header><h1>kolk</h1><span class="sub">this session, live</span><a class="sub" href="/v1/client/sessions">every session</a></header>`)
		b.WriteString(`<iframe src="/v1/client/stream" title="transcript"></iframe>`)
		if tier == devices.TierSteer {
			b.WriteString(`<form method="post" action="/v1/client/turn"><textarea name="prompt" placeholder="Ask kolk…" required></textarea><button type="submit">Send</button></form>`)
		} else {
			b.WriteString(`<p class="sub" style="padding:.6rem 1rem">This device may watch but not act; pair it again or promote it from the machine running the session.</p>`)
		}
		b.WriteString(`</body></html>`)
		_, _ = w.Write([]byte(b.String()))
	}
}

// clientManifestHandler makes the page installable where the browser allows
// a manifest without a service worker.
func clientManifestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "kolkrabbi", "short_name": "kolk", "start_url": "/v1/client", "display": "standalone",
			"background_color": "#ffffff", "theme_color": "#7c5cff",
		})
	}
}

// clientTurnHandler is the form post: same origin, steer tier, then the
// same starter the API uses, and back to the page.
func clientTurnHandler(token string, store *devices.Store, starter TurnStarter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		tier, ok := clientTier(r, token, store)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if tier != devices.TierSteer {
			http.Error(w, "this device may watch but not act", http.StatusForbidden)
			return
		}
		if starter == nil {
			http.Error(w, "this server is not attached to a session", http.StatusNotImplemented)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxTurnRequestBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		prompt := strings.TrimSpace(r.PostForm.Get("prompt"))
		if prompt == "" {
			http.Redirect(w, r, clientPrefix, http.StatusSeeOther)
			return
		}
		if err := starter.StartTurn(prompt); err != nil {
			http.Error(w, "the session could not take a turn now", http.StatusConflict)
			return
		}
		http.Redirect(w, r, clientPrefix, http.StatusSeeOther)
	}
}

// clientStreamHandler is GET /v1/client/stream: chunked HTML, one line per
// event, flushed as it arrives, no script. A browser renders it as it comes.
func clientStreamHandler(b *bus.Bus, pingInterval time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		sub, err := b.Subscribe(0)
		if err != nil {
			http.Error(w, "subscribe error", http.StatusInternalServerError)
			return
		}
		defer sub.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><style>`+clientCSS+`</style></head><body>`)
		flusher.Flush()
		write := func(env protocol.Envelope) {
			if line := clientLine(env); line != "" {
				fmt.Fprint(w, line)
				flusher.Flush()
			}
		}
		for _, env := range sub.Replay() {
			write(env)
		}
		if pingInterval <= 0 {
			pingInterval = 15 * time.Second
		}
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, "<!-- -->")
				flusher.Flush()
			case env, ok := <-sub.Events():
				if !ok {
					return
				}
				write(env)
			}
		}
	}
}

// clientLine renders one event as a person reads it. Content the model chose
// is escaped; nothing here is a style hook for it.
func clientLine(env protocol.Envelope) string {
	var fields map[string]any
	_ = json.Unmarshal(env.Data, &fields)
	text := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := fields[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch env.Type {
	case protocol.EventMessageDelta:
		return `<span class="ev ev-delta">` + html.EscapeString(text("text", "delta")) + `</span>`
	case protocol.EventTurnStarted:
		return `<p class="ev ev-turn">❯ ` + html.EscapeString(text("input")) + `</p>`
	case protocol.EventTurnFinished:
		return `<p class="ev ev-turn">· turn finished` + reasonSuffix(text("reason")) + `</p>`
	case protocol.EventProviderLimit:
		return `<p class="ev ev-err">◆ ` + html.EscapeString(text("kind")+" "+text("action")) + `</p>`
	}
	if strings.HasPrefix(string(env.Type), "tool.") {
		return `<p class="ev ev-tool">→ ` + html.EscapeString(text("name", "tool", "summary")) + `</p>`
	}
	return ""
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + html.EscapeString(reason) + ")"
}

// clientSessionsHandler is GET /v1/client/sessions: every session on the
// machine as the dash draws it — blocked first, live, cost, the checkout it
// shares, what source control is doing — for any paired device. Steering a
// session other than the one this server is attached to is not here: a
// session is steered through its own server.
func clientSessionsHandler(token string, store *devices.Store, sessions func(context.Context) ([]dash.SessionCard, []dash.SharedCheckout)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := clientTier(r, token, store); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var b strings.Builder
		b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>kolk · sessions</title><style>` + clientCSS + dash.CSS() + `</style></head><body>`)
		b.WriteString(`<header><h1>kolk</h1><span class="sub">every session on this machine</span><a class="sub" href="/v1/client">this session</a></header><main style="padding:0 1rem;overflow:auto">`)
		if sessions == nil {
			b.WriteString(`<p class="sub">This server is not attached to a machine's sessions.</p>`)
		} else {
			cards, shared := sessions(r.Context())
			if len(cards) == 0 {
				b.WriteString(`<p class="sub">No sessions yet.</p>`)
			} else {
				b.WriteString(dash.Sessions(cards, shared))
			}
		}
		b.WriteString(`</main></body></html>`)
		_, _ = w.Write([]byte(b.String()))
	}
}
