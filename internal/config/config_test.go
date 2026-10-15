package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	in := &Config{
		CycleTimeoutMS: 900,
		Apps:           map[string]string{"d": "com.vivaldi.Vivaldi"},
		Actions:        map[string]string{"h": "move left", "l": "move right"},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.CycleTimeoutMS != 900 {
		t.Errorf("timeout = %d, want 900", out.CycleTimeoutMS)
	}
	if got := out.Apps["d"]; got != "com.vivaldi.Vivaldi" {
		t.Errorf("app d = %q", got)
	}
	if got := out.Actions["h"]; got != "move left" {
		t.Errorf("action h = %q", got)
	}
	if got := out.Actions["l"]; got != "move right" {
		t.Errorf("action l = %q", got)
	}
}

func TestLoadRejectsDuplicateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &Config{
		CycleTimeoutMS: 1200,
		Apps:           map[string]string{"d": "com.x"},
		Actions:        map[string]string{"d": "move left"},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for a key in both [apps] and [actions]")
	}
}
