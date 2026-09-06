package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

// pairHandler redeems a pairing code for a device token.
//
// This is the one route that cannot require a credential, because handing out
// the first credential is what it is for. It is not in openRoutes for that
// reason: it is not open, it exists only while a person has armed it, and it
// answers 404 the rest of the time.
//
// 404 rather than 401 while unarmed is deliberate. An unarmed machine should
// not advertise that pairing is something it does — a 401 confirms the endpoint
// is there and worth coming back to.
func pairHandler(pairing *devices.Pairing, store *devices.Store, file string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pairing == nil || store == nil || !pairing.Armed() {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			// A GET renders the form and pairs nothing — a GET that paired
			// would be a GET a link can trigger. The form is the browser's
			// way in while armed: no script, no token in any URL (I26.7).
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>pair with kolk</title><style>body{font:15px/1.5 ui-monospace,Menlo,monospace;margin:2rem auto;max-width:28rem;padding:0 1rem}input,button{font:inherit;padding:.4rem}label{display:block;margin:.6rem 0}</style></head><body><h1>pair with kolk</h1><p>Type the six-digit code the session printed.</p><form method="post" action="/v1/pair"><label>code <input name="code" inputmode="numeric" autocomplete="one-time-code" required></label><label>this device <input name="label" placeholder="my phone"></label><button type="submit">Pair</button></form></body></html>`))
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Code  string `json:"code"`
			Label string `json:"label"`
		}
		fromForm := strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
		if fromForm {
			r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			body.Code, body.Label = r.PostForm.Get("code"), r.PostForm.Get("label")
		} else if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad_request","message":"body must be JSON"}`, http.StatusBadRequest)
			return
		}

		switch err := pairing.Redeem(body.Code); {
		case errors.Is(err, devices.ErrNotArmed), errors.Is(err, devices.ErrExpired):
			http.NotFound(w, r)
			return
		case err != nil:
			// Deliberately does not say how many attempts are left: a counter
			// is a hint about how hard to keep trying.
			http.Error(w, `{"error":"forbidden","message":"that pairing code is not right"}`, http.StatusForbidden)
			return
		}

		label := body.Label
		if label == "" {
			label = "unnamed device"
		}
		// Read, always. A device is promoted deliberately from the machine
		// running the session, never by asking for it over the network.
		device, token, err := store.Add(label, devices.TierRead)
		if err != nil {
			http.Error(w, `{"error":"internal","message":"could not create the device"}`, http.StatusInternalServerError)
			return
		}
		if file != "" {
			if err := store.Save(file); err != nil {
				http.Error(w, `{"error":"internal","message":"could not save the device"}`, http.StatusInternalServerError)
				return
			}
		}

		if fromForm {
			// The browser keeps the token as the client cookie and is sent to
			// the page; the token is shown to nobody and put in no URL.
			setClientCookie(w, r, token)
			http.Redirect(w, r, clientPrefix, http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"device_id": device.ID,
			"tier":      string(device.Tier),
		})
	})
}
