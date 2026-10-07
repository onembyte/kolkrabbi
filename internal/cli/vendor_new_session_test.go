package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/session"
)

// Found by the round-5 vendor-recovery verifier, on the agent the CLI builds:
// /new keeps the Claude backend, so the vendor conversation the new session
// runs in must be recorded in the new session's file, and the old session's
// must keep its own. A note bound to the session the backend was built for
// told the old session about the new conversation and left the new one blank,
// so after a restart the new session's saved work could not be continued.
func TestNewRecordsTheNewConversationInTheNewSession(t *testing.T) {
	t.Run("the startup backend", func(t *testing.T) { checkNewRecordsTheNewConversation(t, "") })
	// A backend built by /model is kept by /new in the same way.
	t.Run("a backend /model built", func(t *testing.T) { checkNewRecordsTheNewConversation(t, "claude-sonnet") })
}

func checkNewRecordsTheNewConversation(t *testing.T, switchTo string) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	bin := t.TempDir()
	side := filepath.Join(bin, "side.log")
	script := "#!/bin/sh\n" + probeSidParse + `while IFS= read -r line; do
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
echo "$sid" >> "` + side + `"
printf '%s\n' "{\"type\":\"result\",\"result\":\"ok\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stored := session.New(dirs.Sessions(), "claude-opus")
	stored.SetConnector("claude")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: stored.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	sessA := ag.Session()
	if switchTo != "" {
		if quit := a.slash(context.Background(), ag, "/model "+switchTo); quit {
			t.Fatal("/model ended the session")
		}
		if got := ag.SessionModel(); got != switchTo {
			t.Fatalf("setup: /model left the session on %q, want %q", got, switchTo)
		}
	}
	if err := ag.RunTurn(context.Background(), "hello in A"); err != nil {
		t.Fatal(err)
	}
	convA := sessA.ProviderStateName()
	if convA == "" {
		t.Fatal("setup: session A recorded no conversation")
	}
	if quit := a.slash(context.Background(), ag, "/new"); quit {
		t.Fatal("/new ended the session")
	}
	sessB := ag.Session()
	if sessB.SessionID() == sessA.SessionID() {
		t.Fatal("setup: /new kept the old session")
	}
	if err := ag.RunTurn(context.Background(), "session B work"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(side)
	ran := strings.Fields(string(raw))
	convB := ran[len(ran)-1]
	if convB == convA {
		t.Fatalf("session B's request ran in session A's conversation %q", convA)
	}
	if got := sessB.ProviderStateName(); got != convB {
		t.Fatalf("session B records %q, want %q, the conversation its request ran in", got, convB)
	}
	if got := sessA.ProviderStateName(); got != convA {
		t.Fatalf("session A now records %q, want its own conversation %q", got, convA)
	}
}
