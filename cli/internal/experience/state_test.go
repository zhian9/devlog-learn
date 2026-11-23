package experience

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTripAndUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState missing: %v", err)
	}
	if len(state.Items) != 0 {
		t.Fatalf("items = %+v", state.Items)
	}
	state.Set("demo", Metadata{Kind: KindInternship, Organization: "ACME", Confirmed: true})
	if err := state.Save(path, time.Now()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	meta, ok := loaded.Get("demo")
	if !ok || meta.Kind != KindInternship || meta.Organization != "ACME" || !meta.Confirmed {
		t.Fatalf("metadata = %+v, ok = %v", meta, ok)
	}
	if !loaded.Unset("demo") {
		t.Fatal("Unset existing returned false")
	}
	if loaded.Unset("demo") {
		t.Fatal("Unset missing returned true")
	}
	if err := loaded.Save(path, time.Now()); err != nil {
		t.Fatalf("Save after unset: %v", err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Get("demo"); ok {
		t.Fatal("unset entry survived")
	}
}

func TestStateRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	if err := os.WriteFile(path, []byte(`{"version": 99, "items": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("unknown version was accepted")
	}
}
