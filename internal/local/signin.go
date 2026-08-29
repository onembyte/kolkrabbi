package local

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// signInBudget bounds one /api/me probe. The server answers locally or
// forwards to ollama.com; either way a second is generous.
const signInBudget = 2 * time.Second

// SignInState is what the local server says about its ollama.com sign-in.
//
// The credential is the server's own key in its home directory; kolk never
// holds it. This probe is how the cloud connector is verified — by asking, not
// by a turn — because a local model answering proves nothing about a sign-in.
type SignInState struct {
	// Known is false when nothing answered: unreachable is not "signed out",
	// and a verifier that read a dead server as a revoked sign-in would
	// un-verify a connector every time the machine slept.
	Known     bool
	SignedIn  bool
	Plan      string
	SignInURL string
}

// SignIn asks the server at addr whether it is signed in to ollama.com.
// POST /api/me answers 200 with the account when it is, and 401 with a
// signin_url when it is not.
func SignIn(ctx context.Context, addr string) SignInState {
	ctx, cancel := context.WithTimeout(ctx, signInBudget)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/me", strings.NewReader("{}"))
	if err != nil {
		return SignInState{}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: signInBudget}).Do(request)
	if err != nil {
		return SignInState{}
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))

	switch response.StatusCode {
	case http.StatusOK:
		var me struct {
			Plan string `json:"plan"`
		}
		_ = json.Unmarshal(body, &me)
		return SignInState{Known: true, SignedIn: true, Plan: me.Plan}
	case http.StatusUnauthorized:
		var refusal struct {
			SignInURL string `json:"signin_url"`
		}
		_ = json.Unmarshal(body, &refusal)
		return SignInState{Known: true, SignInURL: refusal.SignInURL}
	default:
		return SignInState{}
	}
}
