package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Clear distinguishes "logged out" from "was not logged in", so logout can say
// which happened rather than claiming to have removed something it did not.
func TestClearReportsWhetherItRemovedAnything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BACHS_CONFIG_DIR", dir)

	if _, err := Save("sk_sandbox_abcdef123456"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	removed, err := Clear()
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !removed {
		t.Error("removed a real config file but reported nothing to remove")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Error("config file still present after Clear")
	}
}

// Running logout twice should be quiet. A missing file is the desired end
// state, not a failure.
func TestClearIsIdempotent(t *testing.T) {
	t.Setenv("BACHS_CONFIG_DIR", t.TempDir())

	removed, err := Clear()
	if err != nil {
		t.Fatalf("Clear on a missing file should not error: %v", err)
	}
	if removed {
		t.Error("reported removing a file that was never there")
	}
}
