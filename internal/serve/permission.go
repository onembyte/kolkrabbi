package serve

import (
	"encoding/json"
	"net/http"

	"github.com/onembyte/kolkrabbi/protocol"
)

// PermissionResolver resolves pending permission requests.
type PermissionResolver interface {
	ResolvePermission(id string, decision protocol.PermissionDecision) error
}

type resolveRequest struct {
	ID       string                      `json:"id"`
	Decision protocol.PermissionDecision `json:"decision"`
}

func permissionResolveHandler(resolver PermissionResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		if resolver == nil {
			http.Error(w, `{"error":"unimplemented","message":"no permission resolver configured"}`, http.StatusNotImplemented)
			return
		}

		var req resolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad_request","message":"invalid json payload"}`, http.StatusBadRequest)
			return
		}

		if req.ID == "" {
			http.Error(w, `{"error":"bad_request","message":"missing id"}`, http.StatusBadRequest)
			return
		}

		switch req.Decision {
		case protocol.PermissionDecisionAllow, protocol.PermissionDecisionAllowSession, protocol.PermissionDecisionDeny:
		default:
			http.Error(w, `{"error":"bad_request","message":"invalid decision; must be allow, allow_session, or deny"}`, http.StatusBadRequest)
			return
		}

		if err := resolver.ResolvePermission(req.ID, req.Decision); err != nil {
			http.Error(w, `{"error":"not_found","message":"permission request not found or expired"}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
