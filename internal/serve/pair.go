package serve

import (
	"encoding/json"
	"errors"
	"net/http"

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
		if r.Method != http.MethodPost {
			// A GET that pairs is a GET a link can trigger.
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Code  string `json:"code"`
			Label string `json:"label"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":     token,
			"device_id": device.ID,
			"tier":      string(device.Tier),
		})
	})
}
