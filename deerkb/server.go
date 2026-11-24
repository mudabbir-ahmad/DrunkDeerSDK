package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

const maxBody = 4 << 20

// updaterName is what the launcher accepts: the DrunkDeer updater, not any program a request names.
var updaterName = regexp.MustCompile(`(?i)(upgrad|updat)`)

type server struct {
	kb    *Keyboard
	store *profileStore
	quit  func()
}

// newMux serves the UI and its JSON API. Only the app's own localhost address is answered, and every
// write must carry X-DeerKB: a plain web page cannot send that header cross-site without a preflight.
func newMux(kb *Keyboard, store *profileStore, quit func(), port int) http.Handler {
	s := &server{kb: kb, store: store, quit: quit}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/layout", s.layout)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/redetect", s.redetect)
	mux.HandleFunc("POST /api/apply", s.apply)
	mux.HandleFunc("POST /api/lighting", s.lighting)
	mux.HandleFunc("POST /api/remap", s.remap)
	mux.HandleFunc("POST /api/live/start", s.liveStart)
	mux.HandleFunc("POST /api/live/stop", s.liveStop)
	mux.HandleFunc("GET /api/live", s.live)
	mux.HandleFunc("POST /api/firmware/launch", s.firmwareLaunch)
	mux.HandleFunc("POST /api/firmware/release", s.firmwareRelease)
	mux.HandleFunc("GET /api/profiles", s.profileList)
	mux.HandleFunc("POST /api/profiles/import", s.profileImport)
	mux.HandleFunc("GET /api/profiles/{name}", s.profileGet)
	mux.HandleFunc("PUT /api/profiles/{name}", s.profilePut)
	mux.HandleFunc("DELETE /api/profiles/{name}", s.profileDelete)
	mux.HandleFunc("POST /api/profiles/{name}/rename", s.profileRename)
	mux.HandleFunc("GET /api/profiles/{name}/export", s.profileExport)
	mux.HandleFunc("POST /api/quit", s.quitApp)

	allowed := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", port): true,
		fmt.Sprintf("localhost:%d", port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-DeerKB") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func statusFor(err error) int {
	var bad badInput
	switch {
	case errors.As(err, &bad):
		return http.StatusBadRequest
	case errors.Is(err, errNotConnected), errors.Is(err, errReadOnly), errors.Is(err, errPresetReadOnly), errors.Is(err, errNameTaken):
		return http.StatusConflict
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func fail(w http.ResponseWriter, err error) {
	reply(w, statusFor(err), map[string]any{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		fail(w, badInput{fmt.Errorf("bad request body: %w", err)})
		return false
	}
	return true
}

// stepsReply sends the hardware steps, plus the error if one stopped the write.
func stepsReply(w http.ResponseWriter, steps []Step, err error) {
	out := map[string]any{"steps": steps}
	if err != nil {
		out["error"] = err.Error()
		reply(w, statusFor(err), out)
		return
	}
	reply(w, http.StatusOK, out)
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *server) layout(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{
		"keys":         isoKeys,
		"leds":         isoLEDs,
		"groups":       keyGroups,
		"effects":      lightEffects,
		"noRemap":      noRemap,
		"defaultRemap": remapToInts(defaultRemap()),
		"keySlots":     keySlots,
	})
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, s.kb.Status())
}

func (s *server) redetect(w http.ResponseWriter, r *http.Request) {
	s.kb.Redetect()
	reply(w, http.StatusOK, s.kb.Status())
}

func (s *server) apply(w http.ResponseWriter, r *http.Request) {
	var e Editor
	if !decode(w, r, &e) {
		return
	}
	steps, err := s.kb.Apply(ApplyRequest{
		Actuation:    e.Actuation,
		Downstroke:   e.Downstroke,
		Upstroke:     e.Upstroke,
		RapidTrigger: e.RapidTrigger,
		Turbo:        e.Turbo,
		Scale:        e.Scale,
	})
	stepsReply(w, steps, err)
}

func (s *server) lighting(w http.ResponseWriter, r *http.Request) {
	var ls LightSettings
	if !decode(w, r, &ls) {
		return
	}
	step, err := s.kb.ApplyLighting(ls)
	stepsReply(w, []Step{step}, err)
}

func (s *server) remap(w http.ResponseWriter, r *http.Request) {
	var layers [][]int
	if !decode(w, r, &layers) {
		return
	}
	t, err := remapFromInts(layers)
	if err != nil {
		fail(w, err)
		return
	}
	steps, err := s.kb.ApplyRemap(t)
	stepsReply(w, steps, err)
}

func (s *server) liveStart(w http.ResponseWriter, r *http.Request) {
	p, err := s.kb.StartLive()
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"precision": p})
}

func (s *server) liveStop(w http.ResponseWriter, r *http.Request) {
	s.kb.StopLive()
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// live returns the latest key depths (mm) for every slot that has reported since live started.
func (s *server) live(w http.ResponseWriter, r *http.Request) {
	raw, err := s.kb.PollTravel(120 * time.Millisecond)
	if err != nil {
		fail(w, err)
		return
	}
	unit := s.kb.Status().Precision.UnitMM
	depth := make(map[int]float64, len(raw))
	for slot, v := range raw {
		depth[slot] = round2(float64(v) * unit)
	}
	reply(w, http.StatusOK, map[string]any{"depth": depth})
}

type firmwareReq struct {
	Path string `json:"path"`
}

// firmwareLaunch starts DrunkDeer's own updater and releases the USB link to it.
// DeerKB does not flash firmware itself.
func (s *server) firmwareLaunch(w http.ResponseWriter, r *http.Request) {
	var req firmwareReq
	if !decode(w, r, &req) {
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = findUpdater()
	}
	if path == "" {
		reply(w, http.StatusNotFound, map[string]any{"error": "no DrunkDeer updater found in Downloads, Desktop or Program Files; browse to FirmwareUpgrade.exe"})
		return
	}
	if !strings.EqualFold(filepath.Ext(path), ".exe") || !updaterName.MatchString(filepath.Base(path)) {
		fail(w, badInput{fmt.Errorf("%s does not look like the DrunkDeer updater (an .exe with 'upgrade' or 'update' in its name)", path)})
		return
	}
	if _, err := os.Stat(path); err != nil {
		fail(w, err)
		return
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		fail(w, err)
		return
	}
	s.kb.Hold(true)
	reply(w, http.StatusOK, map[string]any{"launched": path})
}

func (s *server) firmwareRelease(w http.ResponseWriter, r *http.Request) {
	s.kb.Hold(false)
	s.kb.Redetect()
	reply(w, http.StatusOK, s.kb.Status())
}

// findUpdater looks in the folders DrunkDeer's installers usually land in, one or two levels deep.
func findUpdater() string {
	home, _ := os.UserHomeDir()
	roots := []string{
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Desktop"),
		`C:\Program Files\DrunkDeer`,
		`C:\Program Files (x86)\DrunkDeer`,
		filepath.Join(home, "AppData", "Local", "DrunkDeer"),
	}
	pats := []string{"FirmwareUpgrade.exe", "Ry_Online_Update*.exe"}
	for _, root := range roots {
		for _, depth := range []string{"", "*", "*/*"} {
			for _, p := range pats {
				if m, _ := filepath.Glob(filepath.Join(root, depth, p)); len(m) > 0 {
					return m[0]
				}
			}
		}
	}
	return ""
}

func (s *server) profileList(w http.ResponseWriter, r *http.Request) {
	saved, err := s.store.list()
	if err != nil {
		fail(w, err)
		return
	}
	presets := []string{}
	for _, p := range presetEditors() {
		presets = append(presets, p.Name)
	}
	reply(w, http.StatusOK, map[string]any{"presets": presets, "saved": saved})
}

// profileImport accepts a web driver profile or an array of them. Every profile is checked before any is saved.
func (s *server) profileImport(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		fail(w, badInput{err})
		return
	}
	list, err := parseProfiles(raw)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range list {
		if strings.TrimSpace(list[i].ShowName) == "" {
			list[i].ShowName = list[i].StorageName
		}
		list[i].ShowName = sanitizeName(list[i].ShowName)
		if err := validateProfile(list[i]); err != nil {
			fail(w, err)
			return
		}
	}
	saved := []string{}
	for _, p := range list {
		name := s.store.uniqueName(p.ShowName)
		p.ShowName = name
		if err := s.store.save(name, p); err != nil {
			fail(w, err)
			return
		}
		saved = append(saved, name)
	}
	reply(w, http.StatusOK, map[string]any{"saved": saved})
}

func (s *server) profileGet(w http.ResponseWriter, r *http.Request) {
	name, err := cleanName(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	if e, ok := presetByName(name); ok {
		reply(w, http.StatusOK, e)
		return
	}
	p, err := s.store.load(name)
	if err != nil {
		fail(w, err)
		return
	}
	e, err := profileToEditor(p)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, e)
}

// profilePut saves the editor under name. An existing profile's rtp block is kept.
func (s *server) profilePut(w http.ResponseWriter, r *http.Request) {
	name, err := cleanName(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	if isPresetName(name) {
		fail(w, errPresetReadOnly)
		return
	}
	var e Editor
	if !decode(w, r, &e) {
		return
	}
	var rtp json.RawMessage
	if old, err := s.store.load(name); err == nil {
		rtp = old.RTP
	}
	p, err := editorToProfile(name, e, rtp)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.store.save(name, p); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"saved": name})
}

func (s *server) profileDelete(w http.ResponseWriter, r *http.Request) {
	name, err := cleanName(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	if isPresetName(name) {
		fail(w, errPresetReadOnly)
		return
	}
	if err := s.store.delete(name); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"deleted": name})
}

// profileRename body: {"name": "New Name"}.
func (s *server) profileRename(w http.ResponseWriter, r *http.Request) {
	name, err := cleanName(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.store.rename(name, req.Name); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"renamed": req.Name})
}

// profileExport sends the saved file as-is, which is already in the web driver's layout.
func (s *server) profileExport(w http.ResponseWriter, r *http.Request) {
	name, err := cleanName(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	data, err := s.store.read(name)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, name))
	_, _ = w.Write(data)
}

func (s *server) quitApp(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.quit()
	}()
}
