package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// LocalSettings configures Kolkrabbi's managed local-model runtime. Every field
// is optional and every one of them is an override for a default that already
// works, so a user who never opens this has a working local setup.
//
// The numbers are pointers because their zero values are meaningful: GPU 0 is a
// real card, and reserving zero headroom is a deliberate choice distinct from
// never having chosen.
// Endpoint is one local model endpoint: a name, an address, and what the
// probe found there (plan 37). The name is the model-id prefix.
type Endpoint struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Kind string `json:"kind"`
	Base string `json:"base"`
}

type LocalSettings struct {
	// Ephemeral defaults to on. ProjectEphemeral overrides it for canonical
	// project roots; preferences live in user storage, never a cloned repo.
	Ephemeral        *bool           `json:"ephemeral,omitempty"`
	ProjectEphemeral map[string]bool `json:"project_ephemeral,omitempty"`
	// Endpoints are the local model endpoints this machine knows about,
	// beyond its own Ollama, which needs no record.
	Endpoints            []Endpoint `json:"endpoints,omitempty"`
	GPUMode              string     `json:"gpu_mode,omitempty"`
	GPUIndex             *int       `json:"gpu_index,omitempty"`
	Quantization         string     `json:"quantization,omitempty"`
	ReservedVRAMFraction *float64   `json:"reserved_vram_fraction,omitempty"`
	ReservedRAMBytes     *uint64    `json:"reserved_ram_bytes,omitempty"`
}

// LocalKeys are the dotted config keys this section accepts, in display order.
var LocalKeys = []string{
	"local.ephemeral",
	"local.gpu_mode",
	"local.gpu_index",
	"local.quantization",
	"local.reserved_vram_fraction",
	"local.reserved_ram_bytes",
}

// SetLocal validates and stores one local setting. Validation happens where the
// value is typed, so a machine-shaped mistake is a message now rather than a
// refused pull much later with no obvious cause.
func SetLocal(cfg *Config, key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "local.ephemeral":
		on, err := ParseOnOff(value)
		if err != nil {
			return err
		}
		cfg.Local.Ephemeral = &on
	case "local.gpu_mode":
		mode := strings.ToLower(value)
		if mode != "auto" && mode != "cpu" && mode != "gpu" {
			return fmt.Errorf("gpu mode %q is not one of auto, cpu or gpu", value)
		}
		cfg.Local.GPUMode = mode
	case "local.gpu_index":
		index, err := strconv.Atoi(value)
		if err != nil || index < 0 {
			return fmt.Errorf("gpu index %q must be zero or a positive whole number", value)
		}
		cfg.Local.GPUIndex = &index
	case "local.quantization":
		if value == "" {
			return fmt.Errorf("quantization cannot be empty; unset it instead")
		}
		cfg.Local.Quantization = value
	case "local.reserved_vram_fraction":
		fraction, err := strconv.ParseFloat(value, 64)
		// Reserving all of it leaves nothing to run in, so this setting could
		// only ever refuse every model.
		if err != nil || fraction < 0 || fraction >= 1 || math.IsNaN(fraction) {
			return fmt.Errorf("reserved vram fraction %q must be at least 0 and below 1", value)
		}
		cfg.Local.ReservedVRAMFraction = &fraction
	case "local.reserved_ram_bytes":
		bytes, err := ParseBytes(value)
		if err != nil {
			return err
		}
		cfg.Local.ReservedRAMBytes = &bytes
	default:
		return fmt.Errorf("unknown config key %q; /config lists every setting", key)
	}
	return nil
}

// GetLocal returns one local setting for display, and whether the key exists at
// all. An empty value for a known key means "unset, inheriting the default".
func GetLocal(cfg *Config, key string) (string, bool) {
	switch key {
	case "local.ephemeral":
		if cfg.Local.Ephemeral == nil {
			return "", true
		}
		return onOff(cfg.Local.Ephemeral), true
	case "local.gpu_mode":
		return cfg.Local.GPUMode, true
	case "local.gpu_index":
		if cfg.Local.GPUIndex == nil {
			return "", true
		}
		return strconv.Itoa(*cfg.Local.GPUIndex), true
	case "local.quantization":
		return cfg.Local.Quantization, true
	case "local.reserved_vram_fraction":
		if cfg.Local.ReservedVRAMFraction == nil {
			return "", true
		}
		return strconv.FormatFloat(*cfg.Local.ReservedVRAMFraction, 'g', -1, 64), true
	case "local.reserved_ram_bytes":
		if cfg.Local.ReservedRAMBytes == nil {
			return "", true
		}
		return strconv.FormatUint(*cfg.Local.ReservedRAMBytes, 10), true
	default:
		return "", false
	}
}

// UnsetLocal returns one setting to its computed default.
func UnsetLocal(cfg *Config, key string) error {
	switch key {
	case "local.ephemeral":
		cfg.Local.Ephemeral = nil
	case "local.gpu_mode":
		cfg.Local.GPUMode = ""
	case "local.gpu_index":
		cfg.Local.GPUIndex = nil
	case "local.quantization":
		cfg.Local.Quantization = ""
	case "local.reserved_vram_fraction":
		cfg.Local.ReservedVRAMFraction = nil
	case "local.reserved_ram_bytes":
		cfg.Local.ReservedRAMBytes = nil
	default:
		return fmt.Errorf("unknown config key %q; /config lists every setting", key)
	}
	return nil
}

// ParseBytes reads a byte size the way a person writes one. Reserved memory in
// raw bytes is a number nobody types correctly, so 4GiB, 4G and 2048 all work.
func ParseBytes(value string) (uint64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("a byte size is required, for example 4GiB")
	}
	digits := strings.TrimRight(trimmed, "aAbBiIeEgGkKmMpPtT \t")
	unit := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, digits)))
	multiplier := uint64(1)
	switch unit {
	case "", "b":
	case "k", "kb", "kib":
		multiplier = 1 << 10
	case "m", "mb", "mib":
		multiplier = 1 << 20
	case "g", "gb", "gib":
		multiplier = 1 << 30
	case "t", "tb", "tib":
		multiplier = 1 << 40
	default:
		return 0, fmt.Errorf("%q is not a byte size; use bytes or a unit like 4GiB", value)
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(digits), 64)
	if err != nil || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("%q is not a byte size; use bytes or a unit like 4GiB", value)
	}
	return uint64(amount * float64(multiplier)), nil
}

// settings renders the local section for `kolk config`. Only keys the user has
// actually set appear: the rest are decided per machine by the hardware probe,
// so printing a fixed default for them would be a guess presented as a fact.
func (l LocalSettings) settings() []Setting {
	rows := make([]Setting, 0, len(LocalKeys))
	value := "on"
	if l.Ephemeral != nil && !*l.Ephemeral {
		value = "off"
	}
	rows = append(rows, Setting{Key: "local.ephemeral", Value: value, Default: l.Ephemeral == nil,
		Summary: "stop Kolk's local runtime when the session closes; off keeps it for this project"})
	// Every key is listed, set or not: one nobody can see is one nobody sets.
	// Unset, Kolkrabbi computes the value from the machine.
	add := func(key, value, summary string) {
		row := Setting{Key: key, Value: value, Summary: summary}
		if value == "" {
			row.Value, row.Default = "computed", true
		}
		rows = append(rows, row)
	}
	add("local.gpu_mode", l.GPUMode, "where local models run: auto · cpu · gpu")
	gpuIndex := ""
	if l.GPUIndex != nil {
		gpuIndex = strconv.Itoa(*l.GPUIndex)
	}
	add("local.gpu_index", gpuIndex, "which GPU runs local models when there are several, counting from 0")
	add("local.quantization", l.Quantization, "the weight format local models are pulled in")
	vram := ""
	if l.ReservedVRAMFraction != nil {
		vram = strconv.FormatFloat(*l.ReservedVRAMFraction, 'g', -1, 64)
	}
	add("local.reserved_vram_fraction", vram, "share of GPU memory kept free for other work, from 0 to below 1")
	ram := ""
	if l.ReservedRAMBytes != nil {
		ram = strconv.FormatUint(*l.ReservedRAMBytes, 10)
	}
	add("local.reserved_ram_bytes", ram, "system memory kept free for other work, in bytes")
	return rows
}

// LocalForProject applies the project override without mutating shared config.
// root is the verified, canonical project directory supplied by the surface.
func (c *Config) LocalForProject(root string) LocalSettings {
	l := c.Local
	if on, ok := l.ProjectEphemeral[root]; ok {
		l.Ephemeral = &on
	}
	return l
}

// EphemeralEnabled is the effective lifetime; absent settings stop at exit.
func (l LocalSettings) EphemeralEnabled() bool { return l.Ephemeral == nil || *l.Ephemeral }

// FindEndpoint returns the endpoint with this name.
func (c *Config) FindEndpoint(name string) (Endpoint, bool) {
	for _, endpoint := range c.Local.Endpoints {
		if endpoint.Name == name {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

// PutEndpoint adds an endpoint or replaces the one with its name.
func (c *Config) PutEndpoint(endpoint Endpoint) {
	for i := range c.Local.Endpoints {
		if c.Local.Endpoints[i].Name == endpoint.Name {
			c.Local.Endpoints[i] = endpoint
			return
		}
	}
	c.Local.Endpoints = append(c.Local.Endpoints, endpoint)
}

// RemoveEndpoint forgets one, reporting whether there was one to forget.
func (c *Config) RemoveEndpoint(name string) bool {
	for i, endpoint := range c.Local.Endpoints {
		if endpoint.Name == name {
			c.Local.Endpoints = append(c.Local.Endpoints[:i], c.Local.Endpoints[i+1:]...)
			return true
		}
	}
	return false
}
