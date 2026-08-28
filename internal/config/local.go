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
type LocalSettings struct {
	GPUMode              string   `json:"gpu_mode,omitempty"`
	GPUIndex             *int     `json:"gpu_index,omitempty"`
	Quantization         string   `json:"quantization,omitempty"`
	ReservedVRAMFraction *float64 `json:"reserved_vram_fraction,omitempty"`
	ReservedRAMBytes     *uint64  `json:"reserved_ram_bytes,omitempty"`
}

// LocalKeys are the dotted config keys this section accepts, in display order.
var LocalKeys = []string{
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
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

// GetLocal returns one local setting for display, and whether the key exists at
// all. An empty value for a known key means "unset, inheriting the default".
func GetLocal(cfg *Config, key string) (string, bool) {
	switch key {
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
		return fmt.Errorf("unknown config key %q", key)
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
	add := func(key, value string) {
		if value != "" {
			rows = append(rows, Setting{Key: key, Value: value, Summary: "local model runtime"})
		}
	}
	add("local.gpu_mode", l.GPUMode)
	if l.GPUIndex != nil {
		add("local.gpu_index", strconv.Itoa(*l.GPUIndex))
	}
	add("local.quantization", l.Quantization)
	if l.ReservedVRAMFraction != nil {
		add("local.reserved_vram_fraction", strconv.FormatFloat(*l.ReservedVRAMFraction, 'g', -1, 64))
	}
	if l.ReservedRAMBytes != nil {
		add("local.reserved_ram_bytes", strconv.FormatUint(*l.ReservedRAMBytes, 10))
	}
	return rows
}
