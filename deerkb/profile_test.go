package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// loadTestProfile reads the web driver's sample export.
func loadTestProfile(t *testing.T) Profile {
	t.Helper()
	raw, err := os.ReadFile("testdata/Profile1.json")
	if err != nil {
		t.Fatal(err)
	}
	list, err := parseProfiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("parseProfiles: got %d profiles, want 1", len(list))
	}
	return list[0]
}

func TestProfileRoundTrip(t *testing.T) {
	src := loadTestProfile(t)
	e, err := profileToEditor(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := editorToProfile(src.ShowName, e, src.RTP)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Keys) != keySlots {
		t.Fatalf("keys: got %d, want %d", len(got.Keys), keySlots)
	}
	for i, k := range got.Keys {
		want := src.Keys[i]
		if k.KeyName != want.KeyName {
			t.Errorf("slot %d: keyname %q, want %q", i, k.KeyName, want.KeyName)
		}
		if k.ActionPoint != round2(want.ActionPoint) || k.Downstroke != round2(want.Downstroke) || k.Upstroke != round2(want.Upstroke) {
			t.Errorf("slot %d: got %+v, want %+v (2 dp)", i, k, want)
		}
	}
}

func TestValidateProfile(t *testing.T) {
	if err := validateProfile(loadTestProfile(t)); err != nil {
		t.Fatalf("sample profile rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Profile)
	}{
		{"125 keys", func(p *Profile) { p.Keys = p.Keys[:keySlots-1] }},
		{"action point below 0.2 mm", func(p *Profile) { p.Keys[5].ActionPoint = 0.1 }},
		{"action point above 3.3 mm", func(p *Profile) { p.Keys[5].ActionPoint = 3.5 }},
	}
	for _, c := range cases {
		p := loadTestProfile(t)
		c.mut(&p)
		if err := validateProfile(p); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestLastWinRoundTrip(t *testing.T) {
	src := loadTestProfile(t)
	e, err := profileToEditor(src)
	if err != nil {
		t.Fatal(err)
	}
	if e.LastWin != "off" || e.AutoMatch {
		t.Fatalf("defaults: last win %q, auto match %t", e.LastWin, e.AutoMatch)
	}
	e.AutoMatch, e.LastWin, e.LastWinReplace = true, "on", true
	e.LastWinPairs = [][2]int{{16, 26}, {17, 27}}
	p, err := editorToProfile(src.ShowName, e, src.RTP)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.RTP) != string(src.RTP) {
		t.Error("rtp block changed")
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back Profile
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, err := profileToEditor(back)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoMatch || got.LastWin != "on" || !got.LastWinReplace || len(got.LastWinPairs) != 2 ||
		got.LastWinPairs[0] != [2]int{16, 26} || got.LastWinPairs[1] != [2]int{17, 27} {
		t.Errorf("round trip lost Last Win fields: %+v", got)
	}
}

func TestLastWinLimits(t *testing.T) {
	cases := []struct {
		name string
		lw   string
		prs  [][2]int
	}{
		{"15 pairs", "on", make([][2]int, 15)},
		{"slot 126", "on", [][2]int{{16, 126}}},
		{"negative slot", "on", [][2]int{{-1, 26}}},
		{"unknown mode", "maybe", nil},
	}
	for _, c := range cases {
		src := loadTestProfile(t)
		e, err := profileToEditor(src)
		if err != nil {
			t.Fatal(err)
		}
		e.LastWin, e.LastWinPairs = c.lw, c.prs
		if _, err := editorToProfile(src.ShowName, e, src.RTP); err == nil {
			t.Errorf("%s: editorToProfile accepted", c.name)
		}
		p := loadTestProfile(t)
		p.DeerKB = &deerkbExtras{Version: 1, Scale: "auto", LastWin: c.lw, LastWinPairs: c.prs}
		if err := validateProfile(p); err == nil {
			t.Errorf("%s: validateProfile accepted", c.name)
		}
	}
	src := loadTestProfile(t)
	e, _ := profileToEditor(src)
	e.LastWin, e.LastWinPairs = "on", make([][2]int, maxLastWinPairs)
	if _, err := editorToProfile(src.ShowName, e, src.RTP); err != nil {
		t.Errorf("14 pairs rejected: %v", err)
	}
}

func TestCleanName(t *testing.T) {
	for _, s := range []string{"../x", "", "a/b", `x\y`, "bad:name", strings.Repeat("a", 41)} {
		if _, err := cleanName(s); err == nil {
			t.Errorf("cleanName(%q): accepted", s)
		}
	}
	if got, err := cleanName("Gaming (2)"); err != nil || got != "Gaming (2)" {
		t.Errorf("cleanName(Gaming (2)) = %q, %v", got, err)
	}
}

func TestSanitizeName(t *testing.T) {
	for _, s := range []string{"../../etc/passwd", "", "   ", "a:b*c?", strings.Repeat("x y ", 40), "-", "(*)"} {
		got := sanitizeName(s)
		if _, err := cleanName(got); err != nil {
			t.Errorf("sanitizeName(%q) = %q, rejected: %v", s, got, err)
		}
	}
}

func TestProfileStore(t *testing.T) {
	s, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := loadTestProfile(t)
	if err := s.save("../x", p); err == nil {
		t.Error("save accepted ../x")
	}
	if err := s.save("Alpha", p); err != nil {
		t.Fatal(err)
	}
	names, err := s.list()
	if err != nil || len(names) != 1 || names[0] != "Alpha" {
		t.Fatalf("list after save: %v, %v", names, err)
	}
	got, err := s.load("Alpha")
	if err != nil || got.ShowName != p.ShowName || len(got.Keys) != keySlots {
		t.Fatalf("load: %+v, %v", got.ShowName, err)
	}
	if err := s.delete("Alpha"); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.list(); len(names) != 0 {
		t.Errorf("list after delete: %v", names)
	}
}

func TestUniqueName(t *testing.T) {
	s, err := newProfileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.uniqueName("X"); got != "X" {
		t.Errorf("free name: got %q, want X", got)
	}
	p := loadTestProfile(t)
	if err := s.save("X", p); err != nil {
		t.Fatal(err)
	}
	if got := s.uniqueName("X"); got != "X (2)" {
		t.Errorf("taken name: got %q, want X (2)", got)
	}
	if err := s.save("X (2)", p); err != nil {
		t.Fatal(err)
	}
	if got := s.uniqueName("X"); got != "X (3)" {
		t.Errorf("two taken: got %q, want X (3)", got)
	}
}
