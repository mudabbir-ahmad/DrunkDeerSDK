package main

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func presetNames() map[string]bool {
	names := map[string]bool{}
	for _, p := range presetEditors() {
		names[p.Name] = true
	}
	return names
}

func TestPresetList(t *testing.T) {
	names := presetNames()
	for _, want := range []string{"Ripple", "Breath"} {
		if !names[want] {
			t.Errorf("preset %q missing", want)
		}
	}
	for _, bad := range []string{"Lighting turn off", "Custom light"} {
		if names[bad] {
			t.Errorf("preset %q should not exist", bad)
		}
	}
}

func TestPresetRoundTrip(t *testing.T) {
	for _, pr := range presetEditors() {
		p, err := editorToProfile(pr.Name, pr.Editor, nil)
		if err != nil {
			t.Errorf("%s: editorToProfile: %v", pr.Name, err)
			continue
		}
		if err := validateProfile(p); err != nil {
			t.Errorf("%s: validateProfile: %v", pr.Name, err)
		}
		if _, err := profileToEditor(p); err != nil {
			t.Errorf("%s: profileToEditor: %v", pr.Name, err)
		}
	}
}

// jsonEqual compares raw JSON ignoring whitespace, since MarshalIndent re-formats embedded raw messages.
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func TestProfileRename(t *testing.T) {
	s, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := loadTestProfile(t)
	src.DeerKB = &deerkbExtras{Version: 1, Scale: "kun", RapidTrigger: true, Turbo: true, Lighting: LightSettings{Effect: "ripple", Brightness: 4, Speed: 2}}
	if err := s.save("Alpha", src); err != nil {
		t.Fatal(err)
	}
	if err := s.rename("Alpha", "Beta Two"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path("Alpha")); !os.IsNotExist(err) {
		t.Errorf("old file still there: %v", err)
	}
	got, err := s.load("Beta Two")
	if err != nil {
		t.Fatal(err)
	}
	if got.ShowName != "Beta Two" || got.StorageName != storageName("Beta Two") {
		t.Errorf("names: show %q storage %q", got.ShowName, got.StorageName)
	}
	if !jsonEqual(t, got.RTP, src.RTP) {
		t.Error("rtp block changed on rename")
	}
	if !reflect.DeepEqual(got.DeerKB, src.DeerKB) {
		t.Errorf("deerkb extras: got %+v, want %+v", got.DeerKB, src.DeerKB)
	}
}

func TestProfileRenameConflict(t *testing.T) {
	s, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := loadTestProfile(t)
	if err := s.save("Alpha", p); err != nil {
		t.Fatal(err)
	}
	if err := s.save("Beta", p); err != nil {
		t.Fatal(err)
	}
	if err := s.rename("Alpha", "Beta"); !errors.Is(err, errNameTaken) {
		t.Errorf("rename onto saved name: got %v, want errNameTaken", err)
	}
	if err := s.rename("Alpha", "Ripple"); err == nil {
		t.Error("rename onto preset name accepted")
	}
	if err := s.rename("Ripple", "Gamma"); !errors.Is(err, errPresetReadOnly) {
		t.Errorf("rename of preset: got %v, want errPresetReadOnly", err)
	}
	if err := s.rename("Missing", "Gamma"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rename of missing profile: got %v, want ErrNotExist", err)
	}
	if _, err := s.load("Alpha"); err != nil {
		t.Errorf("Alpha lost after failed rename: %v", err)
	}
}

func TestPresetNameReserved(t *testing.T) {
	s, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := loadTestProfile(t)
	for _, name := range []string{"Ripple", "ripple", "BREATH"} {
		if err := s.save(name, p); err == nil {
			t.Errorf("save(%q) accepted a preset name", name)
		}
	}
	if got := s.uniqueName("Ripple"); got != "Ripple (2)" {
		t.Errorf("uniqueName(Ripple) = %q, want Ripple (2)", got)
	}
}
