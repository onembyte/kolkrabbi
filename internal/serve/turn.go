package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/onembyte/kolkrabbi/protocol"
)

// TurnStarter is the seam a host supplies so a remote device can ask a session
// to do something.
//
// It is a port for the same reason PermissionResolver is: `kolk serve` owns a
// bus, a device store and a socket, and **no agent**. A standalone server has
// nothing to steer, and the honest answer to a turn arriving there is to say
// so — not to spawn a session, which would be the supervisor that items 27 and
// 29 each refused, and not to pretend it worked.
//
// A process that does own a session mounts this handler itself and supplies a
// starter. What that starter must not do is run the turn its own way: it hands
// the prompt to the same agent a local prompt reaches, so the permission tier,
// the hardline floor and the doom-loop guard all apply unchanged. A remote turn
// is a turn.
type TurnStarter interface {
	StartTurn(prompt string) error
}

// maxTurnRequestBytes bounds what will even be read. The command's own limit is
// 32 KiB; this is the reader's guard against a body that never ends, which is a
// different failure from a prompt that is too long.
const maxTurnRequestBytes = 64 * 1024

func turnStartHandler(starter TurnStarter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if starter == nil {
			http.Error(w, `{"error":"unimplemented","message":"this server is not attached to a session, so there is nothing to ask"}`,
				http.StatusNotImplemented)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxTurnRequestBytes+1))
		if err != nil {
			http.Error(w, `{"error":"bad_request","message":"could not read the request"}`, http.StatusBadRequest)
			return
		}
		if len(body) > maxTurnRequestBytes {
			http.Error(w, `{"error":"bad_request","message":"request too large"}`, http.StatusBadRequest)
			return
		}

		// The protocol's validator is the server's rule too. Two copies of
		// "what is a valid turn.start" would be two chances to disagree, and
		// the one that matters is the one the contract publishes.
		if err := protocol.ValidateCommand(protocol.CommandTurnStart, body); err != nil {
			http.Error(w, `{"error":"bad_request","message":"invalid turn.start command"}`, http.StatusBadRequest)
			return
		}

		var command protocol.TurnStartCommand
		if err := json.Unmarshal(body, &command); err != nil {
			http.Error(w, `{"error":"bad_request","message":"invalid json payload"}`, http.StatusBadRequest)
			return
		}
		if err := starter.StartTurn(strings.TrimSpace(command.Prompt)); err != nil {
			http.Error(w, `{"error":"conflict","message":"the session could not take a turn now"}`, http.StatusConflict)
			return
		}
		// Accepted, not completed: what the turn does arrives on /v1/events
		// like everything else a session does.
		w.WriteHeader(http.StatusAccepted)
	}
}
