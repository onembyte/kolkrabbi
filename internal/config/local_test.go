package config

import "testing"

func TestSetLocalValidatesGPUMode(t *testing.T) {
	cfg := &Config{}
	for _, mode := range []string{"auto", "cpu", "gpu", "GPU"} {
		if err := SetLocal(cfg, "local.gpu_mode", mode); err != nil {
			t.Fatalf("%s rejected: %v", mode, err)
		}
	}
	if cfg.Local.GPUMode != "gpu" {
		t.Fatalf("stored mode = %q, want it normalised", cfg.Local.GPUMode)
	}
	if err := SetLocal(cfg, "local.gpu_mode", "turbo"); err == nil {
		t.Fatal("an unknown gpu mode must be rejected at the point it is typed")
	}
}

func TestSetLocalKeepsReservedFractionBelowOne(t *testing.T) {
	cfg := &Config{}
	if err := SetLocal(cfg, "local.reserved_vram_fraction", "0.15"); err != nil {
		t.Fatal(err)
	}
	if cfg.Local.ReservedVRAMFraction == nil || *cfg.Local.ReservedVRAMFraction != 0.15 {
		t.Fatalf("fraction = %+v", cfg.Local.ReservedVRAMFraction)
	}
	// Reserving everything leaves nothing to run in, which is a setting that
	// can only ever refuse every model.
	for _, bad := range []string{"1", "1.5", "-0.1", "half"} {
		if err := SetLocal(cfg, "local.reserved_vram_fraction", bad); err == nil {
			t.Fatalf("%q was accepted as a reserved fraction", bad)
		}
	}
}

func TestSetLocalAcceptsHumanByteSizes(t *testing.T) {
	cfg := &Config{}
	for input, want := range map[string]uint64{
		"4GiB":      4 << 30,
		"4G":        4 << 30,
		"512MiB":    512 << 20,
		"2048":      2048,
		"1.5GiB":    1610612736,
		" 8 GiB \t": 8 << 30,
	} {
		if err := SetLocal(cfg, "local.reserved_ram_bytes", input); err != nil {
			t.Fatalf("%q rejected: %v", input, err)
		}
		if cfg.Local.ReservedRAMBytes == nil || *cfg.Local.ReservedRAMBytes != want {
			t.Fatalf("%q stored %+v, want %d", input, cfg.Local.ReservedRAMBytes, want)
		}
	}
	for _, bad := range []string{"-1", "lots", "4XB", ""} {
		if err := SetLocal(cfg, "local.reserved_ram_bytes", bad); err == nil {
			t.Fatalf("%q was accepted as a byte size", bad)
		}
	}
}

func TestSetLocalRejectsANegativeGPUIndex(t *testing.T) {
	cfg := &Config{}
	if err := SetLocal(cfg, "local.gpu_index", "1"); err != nil {
		t.Fatal(err)
	}
	if cfg.Local.GPUIndex == nil || *cfg.Local.GPUIndex != 1 {
		t.Fatalf("index = %+v", cfg.Local.GPUIndex)
	}
	// Zero is a real GPU, so the field is a pointer and zero must survive.
	if err := SetLocal(cfg, "local.gpu_index", "0"); err != nil {
		t.Fatal(err)
	}
	if cfg.Local.GPUIndex == nil || *cfg.Local.GPUIndex != 0 {
		t.Fatalf("index = %+v, want a stored zero", cfg.Local.GPUIndex)
	}
	if err := SetLocal(cfg, "local.gpu_index", "-1"); err == nil {
		t.Fatal("a negative gpu index must be rejected")
	}
}

func TestGetAndUnsetLocalRoundTrip(t *testing.T) {
	cfg := &Config{}
	if got, ok := GetLocal(cfg, "local.gpu_mode"); ok && got != "" {
		t.Fatalf("unset key reported %q", got)
	}
	if err := SetLocal(cfg, "local.quantization", "Q4_K_M"); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetLocal(cfg, "local.quantization"); got != "Q4_K_M" {
		t.Fatalf("quantization = %q", got)
	}
	if err := UnsetLocal(cfg, "local.quantization"); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetLocal(cfg, "local.quantization"); got != "" {
		t.Fatalf("quantization survived unset: %q", got)
	}
}

func TestLocalKeysAreRejectedWhenUnknown(t *testing.T) {
	cfg := &Config{}
	if err := SetLocal(cfg, "local.turbo", "yes"); err == nil {
		t.Fatal("an unknown local key must be rejected, not silently stored")
	}
	if _, ok := GetLocal(cfg, "local.turbo"); ok {
		t.Fatal("an unknown local key must not report a value")
	}
}
