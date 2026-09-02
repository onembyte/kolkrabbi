package agentcli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The gateway's anthropic/ rows on 2026-09-02, abridged: what a preview is
// built from.
func anthropicGateway() []provider.ModelInfo {
	mk := func(id string, ctx int) provider.ModelInfo {
		return provider.ModelInfo{ID: id, Name: id, ContextLength: ctx}
	}
	return []provider.ModelInfo{
		mk("anthropic/claude-opus-5-fast", 1000000),
		mk("anthropic/claude-opus-5", 1000000),
		mk("anthropic/claude-opus-5:batch", 1000000),
		mk("anthropic/claude-sonnet-5", 1000000),
		mk("anthropic/claude-fable-5", 1000000),
		mk("anthropic/claude-opus-4.8", 1000000),
		mk("anthropic/claude-opus-4.5", 200000),
		mk("anthropic/claude-haiku-4.5", 200000),
		mk("anthropic/claude-sonnet-4.6", 1000000),
		mk("anthropic/claude-3-haiku", 200000),
		mk("anthropic/claude-3.5-sonnet", 200000),
		mk("openai/gpt-5.6-sol", 1050000),
	}
}

// One row per family the CLI names, strongest first; the exact ids behind
// each newest first; variants out; the vendor's efforts on; every row
// unverified until a turn says otherwise.
func TestClaudePreviewGroupsTheGatewayByTheCLIsFamilies(t *testing.T) {
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	catalog, err := ClaudePreviewLister{Gateway: anthropicGateway(), Version: "2.1.258", Now: func() time.Time { return at }}.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Vendor != "claude" || catalog.VendorVersion != "2.1.258" || !catalog.FetchedAt.Equal(at) {
		t.Fatalf("header = %+v", catalog)
	}
	var rows []string
	for _, model := range catalog.Models {
		rows = append(rows, model.ID)
		if model.Status != provider.StatusUnverified {
			t.Errorf("%s status = %q, want unverified before any turn", model.ID, model.Status)
		}
		if strings.Join(model.Efforts, ",") != "low,medium,high,xhigh,max" {
			t.Errorf("%s efforts = %v", model.ID, model.Efforts)
		}
	}
	if got := strings.Join(rows, ","); got != "claude-fable,claude-opus,claude-sonnet,claude-haiku" {
		t.Fatalf("rows = %s, want one per family strongest first", got)
	}

	opus, _ := catalog.Find("claude-opus")
	if got := strings.Join(opus.ExactIDs, ","); got != "anthropic/claude-opus-5,anthropic/claude-opus-4.8,anthropic/claude-opus-4.5" {
		t.Fatalf("opus exact ids = %s, want newest first with -fast and :batch out", got)
	}
	if opus.Rank != 2 || opus.Context != 1000000 || opus.Display != "opus" {
		t.Fatalf("opus = %+v", opus)
	}
	haiku, _ := catalog.Find("claude-haiku")
	if got := strings.Join(haiku.ExactIDs, ","); got != "anthropic/claude-haiku-4.5,anthropic/claude-3-haiku" {
		t.Fatalf("haiku exact ids = %s, want the legacy spelling recognised and ordered", got)
	}
	sonnet, _ := catalog.Find("claude-sonnet")
	if got := strings.Join(sonnet.ExactIDs, ","); got != "anthropic/claude-sonnet-5,anthropic/claude-sonnet-4.6,anthropic/claude-3.5-sonnet" {
		t.Fatalf("sonnet exact ids = %s", got)
	}
	if fable, _ := catalog.Find("claude-fable"); fable.Rank != 1 {
		t.Fatalf("fable = %+v, want the top rank", fable)
	}
}

func TestClaudePreviewNeedsAGatewayAndAKnownFamily(t *testing.T) {
	if _, err := (ClaudePreviewLister{}).Discover(context.Background()); err == nil {
		t.Fatal("a preview with no gateway succeeded")
	}
	unknown := []provider.ModelInfo{{ID: "anthropic/claude-nova-6"}, {ID: "openai/gpt-5.6-sol"}}
	if _, err := (ClaudePreviewLister{Gateway: unknown}).Discover(context.Background()); err == nil {
		t.Fatal("a family the CLI does not name was invented as a row")
	}
}

// Only the vendor's own refusal phrasing retires a model; a network error or
// a limit must not.
func TestIsModelRefusalMatchesOnlyTheVendorsPhrasing(t *testing.T) {
	for _, refused := range []error{
		errModelRefused,
		errors.New("claude exited unsuccessfully: [claude-code:unrecognized_model] {\"model\":\"nope\"}"),
		errors.New("There's an issue with the selected model (nope). It may not exist or you may not have access to it."),
	} {
		if !IsModelRefusal(refused) {
			t.Errorf("not recognised as a refusal: %v", refused)
		}
	}
	for _, other := range []error{
		nil,
		errors.New("dial tcp: connection refused"),
		errors.New("the model is over its usage limit; try again at 18:00"),
		errors.New("There's an issue with the selected model: rate limited"),
	} {
		if IsModelRefusal(other) {
			t.Errorf("treated as a refusal: %v", other)
		}
	}
}

var _ provider.ModelLister = ClaudePreviewLister{}
