package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// defaultRTP is the rtp block the web driver writes when there are no Last Win or RDT settings.
const defaultRTP = `{"rdt_rtpSettings":[{"mainKey":{},"triggerKey":{},"x2Reset":0.05,"x2Active":1.5,"isRdtEnabled":true}],"rdt_watch_change":false,"lw_temp_list":[[]],"lw_rtpSettings":[],"lw_watch_change":false,"rtp_model":"","rdt_open":false,"lw_open":false}`

const maxProfiles = 50

// Profile is the web driver's export format (testdata/Profile1.json). The "deerkb" block is ours; the web
// driver ignores it. "rtp" stays raw JSON so Last Win and RDT settings survive an import and export.
type Profile struct {
	StorageName string          `json:"storagename"`
	ShowName    string          `json:"showname"`
	Keys        []profileKey    `json:"keys_array"`
	RTP         json.RawMessage `json:"rtp"`
	IsActive    bool            `json:"isActive"`
	DeerKB      *deerkbExtras   `json:"deerkb,omitempty"`
}

// profileKey is one firmware slot. Values are mm.
type profileKey struct {
	KeyName     string  `json:"keyname"`
	ActionPoint float64 `json:"action_point"`
	Downstroke  float64 `json:"downstroke"`
	Upstroke    float64 `json:"upstroke"`
}

// deerkbExtras holds the settings the web driver's format has no place for.
type deerkbExtras struct {
	Version      int           `json:"version"`
	Scale        string        `json:"scale"`
	RapidTrigger bool          `json:"rapid_trigger"`
	Turbo        bool          `json:"turbo"`
	Lighting     LightSettings `json:"lighting"`
	Remap        [][]int       `json:"remap,omitempty"`
	AutoMatch    bool          `json:"auto_match"`
	LastWin      string        `json:"last_win"`
	// LastWinReplace and LastWinPairs are the Last Win settings the rtp block cannot hold for us.
	LastWinReplace bool     `json:"last_win_replace"`
	LastWinPairs   [][2]int `json:"last_win_pairs,omitempty"`
}

// Editor is the state the UI edits. Actuation, Downstroke and Upstroke hold mm per firmware slot.
type Editor struct {
	Actuation    []float64     `json:"actuation"`
	Downstroke   []float64     `json:"downstroke"`
	Upstroke     []float64     `json:"upstroke"`
	RapidTrigger bool          `json:"rapid_trigger"`
	Turbo        bool          `json:"turbo"`
	Scale        string        `json:"scale"`
	Lighting     LightSettings `json:"lighting"`
	Remap        [][]int       `json:"remap"`
	AutoMatch    bool          `json:"auto_match"`
	// LastWin is "on" or "off". Last Win needs beta RT-plus firmware, per the vendor.
	LastWin        string   `json:"last_win"`
	LastWinReplace bool     `json:"last_win_replace"`
	LastWinPairs   [][2]int `json:"last_win_pairs"`
}

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9_()-]([A-Za-z0-9 _()-]{0,38}[A-Za-z0-9_()-])?$`)
	junkRE = regexp.MustCompile(`[^A-Za-z0-9 _()-]+`)
)

// cleanName accepts a profile name only if it is safe to use as a file name.
func cleanName(s string) (string, error) {
	if !nameRE.MatchString(s) {
		return "", badInput{fmt.Errorf("profile name %q must be 1-40 letters, digits, spaces, _ ( ) or -", s)}
	}
	return s, nil
}

// sanitizeName turns an imported name into one cleanName accepts.
func sanitizeName(s string) string {
	s = strings.Join(strings.Fields(junkRE.ReplaceAllString(s, "")), " ")
	if len(s) > 36 {
		s = strings.TrimSpace(s[:36])
	}
	if s == "" {
		return "Imported"
	}
	return s
}

func validScale(s string) bool {
	switch s {
	case "", "auto", "standard", "kun":
		return true
	}
	return false
}

// maxLastWinPairs is the size of the keyboard's Last Win pair table.
const maxLastWinPairs = 14

// checkLastWin rejects Last Win settings the keyboard cannot take. The vendor says Last Win needs beta RT-plus firmware.
func checkLastWin(lastWin string, pairs [][2]int) error {
	if lastWin != "" && lastWin != "off" && lastWin != "on" {
		return badInput{fmt.Errorf("unknown last win %q", lastWin)}
	}
	if len(pairs) > maxLastWinPairs {
		return badInput{fmt.Errorf("%d last win pairs, the maximum is %d", len(pairs), maxLastWinPairs)}
	}
	for _, p := range pairs {
		for _, slot := range p {
			if slot < 0 || slot >= keySlots {
				return badInput{fmt.Errorf("last win slot %d is outside 0-%d", slot, keySlots-1)}
			}
		}
	}
	return nil
}

func checkLighting(ls LightSettings) error {
	if _, ok := findEffect(ls.Effect); !ok {
		return badInput{fmt.Errorf("unknown lighting effect %q", ls.Effect)}
	}
	if ls.Brightness < 0 || ls.Brightness > 9 || ls.Speed < 0 || ls.Speed > 9 {
		return badInput{fmt.Errorf("brightness and speed must be 0-9")}
	}
	if ls.Tint != "" {
		if _, _, err := parseTint(ls.Tint); err != nil {
			return err
		}
	}
	_, err := parseColors(ls.Colors)
	return err
}

func defaultLighting() LightSettings {
	return LightSettings{Effect: "always", Brightness: 9, Speed: 5}
}

// validateProfile rejects anything the keyboard could not take, so bad files fail on import, not on apply.
func validateProfile(p Profile) error {
	if len(p.Keys) != keySlots {
		return badInput{fmt.Errorf("profile %q has %d keys, want %d", p.ShowName, len(p.Keys), keySlots)}
	}
	for i, k := range p.Keys {
		if !(k.ActionPoint >= minMM && k.ActionPoint <= stdMaxMM) {
			return badInput{fmt.Errorf("slot %d (%s): action point %g is outside %g-%g mm", i, k.KeyName, k.ActionPoint, minMM, stdMaxMM)}
		}
		if !(k.Downstroke >= 0 && k.Downstroke <= stdMaxMM) || !(k.Upstroke >= 0 && k.Upstroke <= stdMaxMM) {
			return badInput{fmt.Errorf("slot %d (%s): rapid trigger value is outside 0-%g mm", i, k.KeyName, stdMaxMM)}
		}
	}
	if x := p.DeerKB; x != nil {
		if !validScale(x.Scale) {
			return badInput{fmt.Errorf("unknown scale %q", x.Scale)}
		}
		if x.Remap != nil {
			if _, err := remapFromInts(x.Remap); err != nil {
				return err
			}
		}
		if x.Lighting.Effect != "" {
			if err := checkLighting(x.Lighting); err != nil {
				return err
			}
		}
		if err := checkLastWin(x.LastWin, x.LastWinPairs); err != nil {
			return err
		}
	}
	return nil
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// unifyISOEnter is the one rule for the ISO Enter pair: the lower slot's actuation, downstroke and upstroke are set to
// the upper slot's. It returns copies, so the caller's tables are not changed. Tables too short to hold the pair are
// returned as copies unchanged; the callers check lengths.
func unifyISOEnter(act, down, up []float64) (a, d, u []float64) {
	a = append([]float64(nil), act...)
	d = append([]float64(nil), down...)
	u = append([]float64(nil), up...)
	hi, lo := isoEnterPair[0], isoEnterPair[1]
	if len(a) > hi && len(d) > hi && len(u) > hi && len(a) > lo && len(d) > lo && len(u) > lo {
		a[lo], d[lo], u[lo] = a[hi], d[hi], u[hi]
	}
	return a, d, u
}

// profileToEditor reads a profile into editor state. Profiles written by the web driver have no deerkb block,
// so rapid trigger is inferred from non-zero press or release values.
func profileToEditor(p Profile) (Editor, error) {
	if err := validateProfile(p); err != nil {
		return Editor{}, err
	}
	e := Editor{
		Actuation:  make([]float64, keySlots),
		Downstroke: make([]float64, keySlots),
		Upstroke:   make([]float64, keySlots),
		Scale:      "auto",
		Lighting:   defaultLighting(),
		Remap:      remapToInts(defaultRemap()),
		LastWin:    "off",
	}
	for i, k := range p.Keys {
		e.Actuation[i], e.Downstroke[i], e.Upstroke[i] = k.ActionPoint, k.Downstroke, k.Upstroke
	}
	e.Actuation, e.Downstroke, e.Upstroke = unifyISOEnter(e.Actuation, e.Downstroke, e.Upstroke)
	for i := range e.Downstroke {
		if e.Downstroke[i] > 0 || e.Upstroke[i] > 0 {
			e.RapidTrigger = true
		}
	}
	if x := p.DeerKB; x != nil {
		e.RapidTrigger, e.Turbo = x.RapidTrigger, x.Turbo
		if x.Scale != "" {
			e.Scale = x.Scale
		}
		if x.Lighting.Effect != "" {
			e.Lighting = x.Lighting
		}
		if x.Remap != nil {
			e.Remap = x.Remap
		}
		e.AutoMatch, e.LastWinReplace, e.LastWinPairs = x.AutoMatch, x.LastWinReplace, x.LastWinPairs
		if x.LastWin != "" {
			e.LastWin = x.LastWin
		}
	}
	return e, nil
}

// storageName is the name the web driver files a profile under: the show name without spaces or punctuation.
func storageName(name string) string {
	return "ddeerA75UK" + strings.NewReplacer(" ", "", "_", "", "-", "", "(", "", ")", "").Replace(name)
}

// editorToProfile writes editor state in the web driver's layout. rtp is kept from the profile it replaces.
func editorToProfile(name string, e Editor, rtp json.RawMessage) (Profile, error) {
	if len(e.Actuation) != keySlots || len(e.Downstroke) != keySlots || len(e.Upstroke) != keySlots {
		return Profile{}, badInput{fmt.Errorf("each table needs %d values", keySlots)}
	}
	if !validScale(e.Scale) {
		return Profile{}, badInput{fmt.Errorf("unknown scale %q", e.Scale)}
	}
	if err := checkLastWin(e.LastWin, e.LastWinPairs); err != nil {
		return Profile{}, err
	}
	if e.Scale == "" {
		e.Scale = "auto"
	}
	if e.LastWin == "" {
		e.LastWin = "off"
	}
	actuation, downstroke, upstroke := unifyISOEnter(e.Actuation, e.Downstroke, e.Upstroke)
	if len(rtp) == 0 {
		rtp = json.RawMessage(defaultRTP)
	}
	p := Profile{
		StorageName: storageName(name),
		ShowName:    name,
		Keys:        make([]profileKey, keySlots),
		RTP:         rtp,
		DeerKB: &deerkbExtras{
			Version:        1,
			Scale:          e.Scale,
			RapidTrigger:   e.RapidTrigger,
			Turbo:          e.Turbo,
			Lighting:       e.Lighting,
			Remap:          e.Remap,
			AutoMatch:      e.AutoMatch,
			LastWin:        e.LastWin,
			LastWinReplace: e.LastWinReplace,
			LastWinPairs:   e.LastWinPairs,
		},
	}
	for i := range p.Keys {
		p.Keys[i] = profileKey{
			KeyName:     webNames[i],
			ActionPoint: round2(actuation[i]),
			Downstroke:  round2(downstroke[i]),
			Upstroke:    round2(upstroke[i]),
		}
	}
	if err := validateProfile(p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// presetProfile is a built-in, read-only profile: one per lighting mode effect. Presets are never written to disk.
type presetProfile struct {
	Name   string
	Editor Editor
}

// errPresetReadOnly is returned when a request tries to change or remove a built-in preset.
var errPresetReadOnly = errors.New("built-in presets are read-only: duplicate it to edit")

// errNameTaken is returned when a rename or save would overwrite a saved profile.
var errNameTaken = errors.New("a profile with that name already exists")

func presetEditors() []presetProfile {
	var out []presetProfile
	for _, eff := range lightEffects {
		if eff.Kind != "mode" {
			continue
		}
		e := Editor{
			Actuation:  make([]float64, keySlots),
			Downstroke: make([]float64, keySlots),
			Upstroke:   make([]float64, keySlots),
			Scale:      "auto",
			Lighting:   LightSettings{Effect: eff.ID, Brightness: 9, Speed: 5},
			Remap:      remapToInts(defaultRemap()),
		}
		for i := range e.Actuation {
			e.Actuation[i] = 2.0
		}
		out = append(out, presetProfile{Name: eff.Label, Editor: e})
	}
	return out
}

// presetByName finds a preset by name, ignoring case as the file system does for saved profiles.
func presetByName(name string) (Editor, bool) {
	for _, p := range presetEditors() {
		if strings.EqualFold(p.Name, name) {
			return p.Editor, true
		}
	}
	return Editor{}, false
}

func isPresetName(name string) bool {
	_, ok := presetByName(name)
	return ok
}

// parseProfiles accepts one profile object or an array of them.
func parseProfiles(raw []byte) ([]Profile, error) {
	raw = bytes.TrimSpace(raw)
	var list []Profile
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, badInput{err}
		}
	} else {
		var p Profile
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, badInput{err}
		}
		list = []Profile{p}
	}
	if len(list) == 0 || len(list) > maxProfiles {
		return nil, badInput{fmt.Errorf("expected 1-%d profiles, got %d", maxProfiles, len(list))}
	}
	return list, nil
}

// profileStore keeps one <name>.json per profile in the app's data folder.
type profileStore struct{ dir string }

func newProfileStore(dir string) (*profileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &profileStore{dir: dir}, nil
}

func (s *profileStore) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}

func (s *profileStore) list() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			if _, err := cleanName(n); err == nil {
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *profileStore) exists(name string) bool {
	if _, err := cleanName(name); err != nil {
		return false
	}
	_, err := os.Stat(s.path(name))
	return err == nil
}

// uniqueName returns base, or base (2), base (3)... so an import never overwrites a saved profile or a preset.
func (s *profileStore) uniqueName(base string) string {
	name := base
	for n := 2; s.exists(name) || isPresetName(name); n++ {
		name = fmt.Sprintf("%s (%d)", base, n)
	}
	return name
}

func (s *profileStore) read(name string) ([]byte, error) {
	if _, err := cleanName(name); err != nil {
		return nil, err
	}
	return os.ReadFile(s.path(name))
}

func (s *profileStore) load(name string) (Profile, error) {
	data, err := s.read(name)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("%s.json is corrupt: %w", name, err)
	}
	return p, nil
}

// save writes through a temp file so a crash cannot leave a half-written profile.
func (s *profileStore) save(name string, p Profile) error {
	if _, err := cleanName(name); err != nil {
		return err
	}
	if isPresetName(name) {
		return badInput{fmt.Errorf("name is reserved for a built-in preset")}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(name) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

func (s *profileStore) delete(name string) error {
	if _, err := cleanName(name); err != nil {
		return err
	}
	return os.Remove(s.path(name))
}

// rename moves a saved profile to newName. The whole profile is kept (rtp and deerkb extras); only its names change.
func (s *profileStore) rename(oldName, newName string) error {
	if _, err := cleanName(oldName); err != nil {
		return err
	}
	if _, err := cleanName(newName); err != nil {
		return err
	}
	if isPresetName(oldName) {
		return errPresetReadOnly
	}
	if isPresetName(newName) {
		return badInput{fmt.Errorf("name is reserved for a built-in preset")}
	}
	// Also catches a case-only rename on Windows, where old and new are the same file.
	if s.exists(newName) {
		return errNameTaken
	}
	p, err := s.load(oldName)
	if err != nil {
		return err
	}
	p.ShowName = newName
	p.StorageName = storageName(newName)
	if err := s.save(newName, p); err != nil {
		return err
	}
	return s.delete(oldName)
}
