package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
)

// E4. Host models are a section of their own under `kolk models`, never rows
// in the gateway list: a host id among gateway ids is a 404 waiting to happen,
// and the reader should see which rows never leave the machine.
func TestModelsListsTheHostsModelsAsTheirOwnSection(t *testing.T) {
	var out strings.Builder
	host := local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}
	models := []local.HostModel{
		{Name: "qwen2.5-coder:7b", Parameters: "7.6B", Quantization: "Q4_K_M", ContextLength: 32768, Tools: true, CapabilitiesKnown: true},
		{Name: "gpt-oss:120b-cloud", Cloud: true, ContextLength: 131072, Tools: true, CapabilitiesKnown: true},
	}
	renderHostModels(&out, host, models, "")
	text := out.String()
	for _, want := range []string{"local · ollama 0.33.1", "ollama/qwen2.5-coder:7b", "32768", "7.6B", "ollama/gpt-oss:120b-cloud", "cloud · via ollama.com"} {
		if !strings.Contains(text, want) {
			t.Errorf("listing lacks %q:\n%s", want, text)
		}
	}

	out.Reset()
	renderHostModels(&out, host, models, "cloud")
	if strings.Contains(out.String(), "qwen2.5-coder") {
		t.Errorf("the filter did not apply to host rows:\n%s", out.String())
	}

	out.Reset()
	renderHostModels(&out, host, nil, "")
	if !strings.Contains(out.String(), "nothing pulled") {
		t.Errorf("an empty server prints no sentence:\n%s", out.String())
	}
}

// Installed but idle is told apart from running, so a user with Ollama on
// PATH learns why nothing is listed rather than seeing an empty section.
func TestModelsSaysWhyAnIdleOllamaListsNothing(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostInstalled, Binary: "/opt/ollama"} }
	a.printHostModels(context.Background(), "", "")
	if !strings.Contains(out.String(), "not running") || !strings.Contains(out.String(), "/opt/ollama") {
		t.Errorf("idle Ollama was not explained:\n%s", out.String())
	}
}

func TestDoctorCountsTheHostsModels(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}
	}
	a.listHostModels = func(context.Context, string, string) ([]local.HostModel, error) {
		return []local.HostModel{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil
	}
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "3 model(s)") {
		t.Errorf("doctor does not count the models:\n%s", out.String())
	}
}

func TestModelsListsAnUnpulledCloudCatalogueRowWithItsPullCommand(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}
	}
	a.listHostModels = func(context.Context, string, string) ([]local.HostModel, error) {
		return []local.HostModel{{Name: "qwen2.5-coder:7b"}}, nil
	}
	a.listCloudCatalog = func(context.Context) ([]local.CloudCatalogModel, error) {
		return []local.CloudCatalogModel{{Name: "gpt-oss:120b"}}, nil
	}
	a.listCloudModels = func(context.Context, string, string, string, []local.CloudCatalogModel) ([]local.HostModel, error) {
		return []local.HostModel{{Name: "gpt-oss:120b-cloud", Cloud: true, NotPulled: true, RemoteHost: "https://ollama.com:443"}}, nil
	}

	a.printHostModels(context.Background(), "", "")
	text := out.String()
	if !strings.Contains(text, "ollama/gpt-oss:120b-cloud") || !strings.Contains(text, "not pulled: ollama pull gpt-oss:120b-cloud") {
		t.Fatalf("kolk models omitted Cloud pull guidance:\n%s", text)
	}
}
