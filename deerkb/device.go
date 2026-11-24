package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sstallion/go-hid"
)

const (
	reportID    = 0x04
	frameLen    = 63 // payload after the report id; every HID report is 64 bytes
	keySlots    = 126
	packetSlots = 59
	packetCount = 3

	cmdIdentity = 0xA0
	cmdCommon   = 0xB5 // rapid trigger and turbo
	cmdKeyPoint = 0xB6 // key-point writes, acked with 0xB6; sub 0x03 toggles travel tracking
	cmdTravel   = 0xB7 // live travel stream
	cmdLastWin  = 0xFC // Last Win mode, replace flag and pair table; acked with 0xFC

	subActuation  = 0x01
	subTracking   = 0x03
	subDownstroke = 0x04
	subUpstroke   = 0x05

	ackTimeout = 500 * time.Millisecond
	writeGap   = 20 * time.Millisecond
	defaultMM  = 2.0
	minMM      = 0.2
	stdMaxMM   = 3.3
	kunMaxMM   = 2.0
)

var (
	errNotConnected = errors.New("keyboard not connected")
	errReadOnly     = errors.New("unsupported model: read-only")
	errNoReply      = errors.New("no reply from keyboard")
)

// badInput marks an error caused by the request itself; the API answers 400.
type badInput struct{ err error }

func (b badInput) Error() string { return b.err.Error() }

// model is what a DrunkDeer identity resolves to. kun* describe the 0.01 mm unit scale.
type model struct {
	name      string
	kunMin    int  // firmware from which 0.01 mm units apply; 0 = never
	kunAlways bool // 0.01 mm units on every firmware
	highPrec  bool // 0xFD protocol: identified and shown, never written
	layout    bool // slot layout implemented here (A75 family only)
}

type idEntry struct {
	id      [3]byte
	variant string
	m       model
}

// Table from protocol/models.yaml. Only the A75 family is writable: it is the only layout this app knows.
var (
	mA75       = model{name: "A75", kunMin: 35, layout: true}
	mA75Pro    = model{name: "A75 Pro", kunMin: 17, layout: true}
	mA75Ultra  = model{name: "A75 Ultra", highPrec: true}
	mA75Master = model{name: "A75 Master", highPrec: true}
	mG75       = model{name: "G75", kunMin: 13}
	mG75JP     = model{name: "G75 JP", kunMin: 6}
	mG65       = model{name: "G65", kunMin: 19}
	mG65Lite   = model{name: "G65 Lite"}
	mG65M1     = model{name: "G65 m1", kunAlways: true}
	mG65M2     = model{name: "G65 m2", kunAlways: true}
	mG65M3     = model{name: "G65 m3", kunAlways: true}
	mG60       = model{name: "G60", kunMin: 21}
	mG60V600   = model{name: "G60 v600", kunAlways: true}
	mX60       = model{name: "X60 Future", highPrec: true}
)

var knownIDs = []idEntry{
	{[3]byte{0x0b, 0x04, 0x01}, "ansi", mA75},
	{[3]byte{0x0b, 0x01, 0x01}, "ansi_alt", mA75},
	{[3]byte{0x0b, 0x04, 0x02}, "iso", mA75},
	{[3]byte{0x0b, 0x04, 0x03}, "ansi", mA75Pro},
	{[3]byte{0x0b, 0x04, 0x04}, "ansi", mA75Ultra},
	{[3]byte{0x0b, 0x05, 0x04}, "ansi", mA75Master},
	{[3]byte{0x0b, 0x04, 0x05}, "ansi", mG75},
	{[3]byte{0x0b, 0x04, 0x07}, "jp", mG75JP},
	{[3]byte{0x0b, 0x02, 0x01}, "ansi", mG65},
	{[3]byte{0x0f, 0x01, 0x01}, "ansi_alt", mG65},
	{[3]byte{0x0b, 0x02, 0x02}, "iso", mG65},
	{[3]byte{0x0b, 0x02, 0x05}, "ansi", mG65Lite},
	{[3]byte{0x0b, 0x02, 0x03}, "ansi", mG65M1},
	{[3]byte{0x0b, 0x10, 0x01}, "ansi", mG65M2},
	{[3]byte{0x0b, 0x12, 0x01}, "ansi", mG65M3},
	{[3]byte{0x0b, 0x03, 0x01}, "ansi", mG60},
	{[3]byte{0x0b, 0x03, 0x02}, "iso", mG60},
	{[3]byte{0x0b, 0x03, 0x03}, "ansi", mG60V600},
	{[3]byte{0x0b, 0x06, 0x05}, "ansi", mX60},
}

// ddProducts is the discovery list from protocol/models.yaml.
var ddProducts = [][2]uint16{
	{0x352D, 0x2382}, {0x352D, 0x2383}, {0x352D, 0x2384}, {0x352D, 0x2386}, {0x352D, 0x2387}, {0x352D, 0x2391},
	{0x05AC, 0x024F}, {0x04D9, 0x2A08}, {0x1A85, 0xFC4F},
}

func lookupModel(b [3]byte) (model, string, bool) {
	for _, e := range knownIDs {
		if e.id == b {
			return e.m, e.variant, true
		}
	}
	return model{name: "Unknown"}, "", false
}

// precisionFor returns the unit scale for a model at a firmware. scale is "auto", "standard" or "kun".
func (m model) precisionFor(fw int, scale string) (Precision, error) {
	if m.highPrec {
		return Precision{Mode: "high-precision", UnitMM: 0.005, Min: minMM, Max: stdMaxMM}, nil
	}
	kun := m.kunAlways || (m.kunMin > 0 && fw >= m.kunMin)
	switch scale {
	case "", "auto":
	case "standard":
		if m.kunAlways {
			return Precision{}, fmt.Errorf("%s only takes 0.01 mm units", m.name)
		}
		kun = false
	case "kun":
		if !m.kunAlways && m.kunMin == 0 {
			return Precision{}, fmt.Errorf("%s has no 0.01 mm units", m.name)
		}
		kun = true
	default:
		return Precision{}, fmt.Errorf("unknown scale %q", scale)
	}
	if kun {
		return Precision{Mode: "kun", UnitMM: 0.01, Min: minMM, Max: kunMaxMM}, nil
	}
	return Precision{Mode: "standard", UnitMM: 0.1, Min: minMM, Max: stdMaxMM}, nil
}

// Precision is the unit scale in effect: one device unit is UnitMM millimetres.
type Precision struct {
	Mode   string  `json:"mode"`
	UnitMM float64 `json:"unit_mm"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// Identity is what the keyboard reports in reply to A0 02.
type Identity struct {
	Bytes            [3]byte
	Firmware         int
	Turbo            bool
	RapidTrigger     bool
	RapidTriggerPlus bool
	LastWin          byte
	AutoMatch        byte
}

// parseIdentity reads a 63-byte frame (the report without its id byte).
func parseIdentity(f []byte) (Identity, error) {
	if len(f) < 33 || f[0] != cmdIdentity || f[1] != 0x02 || f[2] != 0x00 {
		return Identity{}, errors.New("not an identity reply")
	}
	return Identity{
		Bytes:            [3]byte{f[4], f[5], f[6]},
		Firmware:         int(f[7]),
		Turbo:            f[15] != 0,
		RapidTrigger:     f[16] != 0,
		RapidTriggerPlus: f[18] != 0,
		LastWin:          f[19],
		AutoMatch:        f[30],
	}, nil
}

func identityFrame(b [3]byte, fw int, turbo, rt bool, lastWin, autoMatch byte) []byte {
	f := padFrame(cmdIdentity, 0x02)
	copy(f[4:7], b[:])
	f[7] = byte(fw)
	f[15], f[16] = boolByte(turbo), boolByte(rt)
	f[19], f[30] = lastWin, autoMatch
	return f
}

// toUnits converts editor values in mm to device bytes, rejecting anything outside [lo, hi].
func toUnits(mm []float64, p Precision, lo, hi float64) ([]byte, error) {
	if len(mm) != keySlots {
		return nil, fmt.Errorf("expected %d values, got %d", keySlots, len(mm))
	}
	out := make([]byte, keySlots)
	for i, v := range mm {
		if !(v >= lo && v <= hi) { // also rejects NaN
			return nil, fmt.Errorf("slot %d: %g mm is outside %g–%g mm", i, v, lo, hi)
		}
		out[i] = byte(math.Min(255, math.Round(v/p.UnitMM)))
	}
	return out, nil
}

func unitsOf(mm float64, p Precision) byte {
	return byte(math.Min(255, math.Round(mm/p.UnitMM)))
}

// keyPointFrames splits 126 slot values into the three 63-byte packets of one B6 write.
// Slots past the table are filled with pad.
func keyPointFrames(sub byte, units []byte, pad byte) [][]byte {
	frames := make([][]byte, packetCount)
	for p := range frames {
		f := padFrame(cmdKeyPoint, sub, 0x00, byte(p))
		for i := 0; i < packetSlots; i++ {
			v := pad
			if slot := p*packetSlots + i; slot < len(units) {
				v = units[slot]
			}
			f[4+i] = v
		}
		frames[p] = f
	}
	return frames
}

// commonFrame is the B5 packet. Byte 10 is the Last Win mode, byte 11 the auto-match flag (SDK CommonConfig).
// Auto match is only sent while rapid trigger is on.
func commonFrame(turbo, rt bool, lastWin byte, autoMatch bool) []byte {
	return padFrame(cmdCommon, 0x00, 0x1E, 0x01, 0x00, 0x00, 0x01, boolByte(turbo), boolByte(rt), 0x00, lastWin, boolByte(autoMatch && rt))
}

// lastWinMode is the SDK's LastWinRapidTriggerMode: bit 0 Last Win, bit 1 Rapid Trigger.
func lastWinMode(lastWin, rt bool) byte {
	return boolByte(lastWin) | boolByte(rt)<<1
}

// lastWinPairFrame is the CreateLwPairs packet: count at byte 3, then 4-byte entries (main, trigger, 0, 0) from byte 4.
func lastWinPairFrame(pairs [][2]int) []byte {
	f := padFrame(cmdLastWin, 0x01, 0x00, byte(len(pairs)))
	for i, p := range pairs {
		f[4+4*i], f[5+4*i] = byte(p[0]), byte(p[1])
	}
	return f
}

// decodeTravel returns the packet index and its 59 raw values from a B7 frame.
func decodeTravel(f []byte) (int, []byte, bool) {
	if len(f) < 4+packetSlots || f[0] != cmdTravel || int(f[3]) >= packetCount {
		return 0, nil, false
	}
	return int(f[3]), f[4 : 4+packetSlots], true
}

func padFrame(b ...byte) []byte {
	f := make([]byte, frameLen)
	copy(f, b)
	return f
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// link is one open channel to the keyboard. Frames are 63 bytes; write adds the report id.
type link interface {
	write(frame []byte) error
	// read returns the next frame, or nil when none arrives within timeout.
	read(timeout time.Duration) ([]byte, error)
	close()
}

type hidLink struct {
	dev  *hid.Device
	logf func(format string, args ...any)
}

func (l *hidLink) write(frame []byte) error {
	buf := make([]byte, 64)
	buf[0] = reportID
	copy(buf[1:], frame)
	if l.logf != nil {
		l.logf("HID TX %x", buf)
	}
	_, err := l.dev.Write(buf)
	return err
}

func (l *hidLink) read(timeout time.Duration) ([]byte, error) {
	buf := make([]byte, 64)
	n, err := l.dev.ReadWithTimeout(buf, timeout)
	if errors.Is(err, hid.ErrTimeout) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if n != 64 || buf[0] != reportID {
		// Dropped frames never reach the callers' matchers, so log them to see what the board sent.
		if l.logf != nil && n > 0 {
			l.logf("HID RX dropped (%d bytes) %x", n, buf[:n])
		}
		return nil, nil
	}
	if l.logf != nil {
		l.logf("HID RX %x", buf)
	}
	return buf[1:], nil
}

func (l *hidLink) close() { _ = l.dev.Close() }

// simLink stands in for the keyboard under --simulate: it answers the handshake, acks writes,
// and streams a fake press on W/A/S/D while tracking is on.
type simLink struct {
	queue    [][]byte
	tracking bool
	turbo    bool
	rt       bool
	pkt      int
	next     time.Time
	start    time.Time
}

// The simulated board is an A75 ISO on firmware 55, so it runs in 0.01 mm units.
var simBytes = [3]byte{0x0b, 0x04, 0x02}

const simFirmware = 55

func (s *simLink) write(f []byte) error {
	switch {
	case f[0] == cmdIdentity && f[1] == 0x02:
		s.queue = append(s.queue, identityFrame(simBytes, simFirmware, s.turbo, s.rt, 0, 0))
	case f[0] == cmdCommon:
		s.turbo, s.rt = f[7] != 0, f[8] != 0
		s.queue = append(s.queue, padFrame(cmdCommon))
	case f[0] == cmdKeyPoint && f[1] == subTracking:
		s.tracking = f[2] != 0
	case f[0] == cmdLastWin:
		s.queue = append(s.queue, padFrame(cmdLastWin))
	case f[0] == cmdKeyPoint:
		s.queue = append(s.queue, padFrame(cmdKeyPoint))
	}
	return nil
}

func (s *simLink) read(timeout time.Duration) ([]byte, error) {
	if len(s.queue) > 0 {
		f := s.queue[0]
		s.queue = s.queue[1:]
		return f, nil
	}
	now := time.Now()
	if !s.tracking || now.Before(s.next) {
		time.Sleep(min(timeout, 5*time.Millisecond))
		return nil, nil
	}
	s.next = now.Add(5 * time.Millisecond)
	f := padFrame(cmdTravel, 0, 0, byte(s.pkt))
	t := now.Sub(s.start).Seconds()
	for i := 0; i < packetSlots; i++ {
		f[4+i] = simRaw(s.pkt*packetSlots+i, t)
	}
	s.pkt = (s.pkt + 1) % packetCount
	return f, nil
}

func (s *simLink) close() {}

// simRaw fakes presses on W, A, S, D: a staggered 0–1.5 mm wave in 0.01 mm units.
func simRaw(slot int, t float64) byte {
	switch slot {
	case 44, 64, 65, 66:
	default:
		return 0
	}
	return byte(math.Max(0, math.Sin(t*math.Pi+float64(slot))) * 150)
}

// Step is one hardware write and its outcome, shown to the user.
type Step struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Warn string `json:"warn,omitempty"`
}

// ApplyRequest is the editor state. Values are mm per firmware slot.
type ApplyRequest struct {
	Actuation    []float64 `json:"actuation"`
	Downstroke   []float64 `json:"downstroke"`
	Upstroke     []float64 `json:"upstroke"`
	RapidTrigger bool      `json:"rapid_trigger"`
	Turbo        bool      `json:"turbo"`
	Scale        string    `json:"scale"`
	AutoMatch    bool      `json:"auto_match"`
	// LastWin is "on" or "off". Last Win needs beta RT-plus firmware, per the vendor.
	LastWin        string   `json:"last_win"`
	LastWinReplace bool     `json:"last_win_replace"`
	LastWinPairs   [][2]int `json:"last_win_pairs"`
}

// Status is what the UI shows about the keyboard.
type Status struct {
	Connected        bool      `json:"connected"`
	Simulated        bool      `json:"simulated"`
	Error            string    `json:"error,omitempty"`
	Writable         bool      `json:"writable"`
	Note             string    `json:"note,omitempty"`
	ModelID          string    `json:"model_id,omitempty"`
	Model            string    `json:"model,omitempty"`
	Variant          string    `json:"variant,omitempty"`
	Firmware         int       `json:"firmware,omitempty"`
	Precision        Precision `json:"precision"`
	Turbo            bool      `json:"turbo"`
	RapidTrigger     bool      `json:"rapid_trigger"`
	RapidTriggerPlus bool      `json:"rapid_trigger_plus"`
	HIDPath          string    `json:"hid_path,omitempty"`
	Interface        string    `json:"interface,omitempty"`
}

// Keyboard owns the single device link. Every exchange holds mu.
type Keyboard struct {
	mu      sync.Mutex
	sim     bool
	logf    func(format string, args ...any)
	link    link
	id      Identity
	model   model
	variant string
	path    string
	iface   string
	live    bool
	held    bool // released to another program (the firmware updater)
	lastErr string
	// travel holds the last raw value seen per slot; travelSeen marks slots that have reported this session.
	travel     [keySlots]byte
	travelSeen [keySlots]bool
}

func NewKeyboard(sim bool, logf func(format string, args ...any)) *Keyboard {
	return &Keyboard{sim: sim, logf: logf}
}

// Run keeps a keyboard attached: it connects when none is, and re-reads identity every 2 s
// so an unplug is noticed even while the UI is idle.
func (k *Keyboard) Run() {
	for {
		k.mu.Lock()
		switch {
		case k.held:
		case k.link == nil:
			k.connectLocked()
		default:
			_ = k.refreshLocked()
		}
		k.mu.Unlock()
		time.Sleep(2 * time.Second)
	}
}

// Redetect re-reads identity, or connects when no keyboard is attached.
func (k *Keyboard) Redetect() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		k.connectLocked()
		return
	}
	_ = k.refreshLocked()
}

// Hold releases the USB link to another program (the firmware updater) and stops polling until released.
func (k *Keyboard) Hold(on bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.held = on
	if on && k.link != nil {
		k.closeLinkLocked()
	}
}

func (k *Keyboard) Status() Status {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := Status{Simulated: k.sim, Error: k.lastErr, Precision: Precision{Mode: "unknown", UnitMM: 0.1, Min: minMM, Max: stdMaxMM}}
	if k.link == nil {
		return s
	}
	p, _ := k.model.precisionFor(k.id.Firmware, "auto")
	s.Connected = true
	s.ModelID = fmt.Sprintf("%02x%02x%02x", k.id.Bytes[0], k.id.Bytes[1], k.id.Bytes[2])
	s.Model, s.Variant, s.Firmware = k.model.name, k.variant, k.id.Firmware
	s.Precision = p
	s.Turbo, s.RapidTrigger, s.RapidTriggerPlus = k.id.Turbo, k.id.RapidTrigger, k.id.RapidTriggerPlus
	s.HIDPath, s.Interface = k.path, k.iface
	s.Writable = k.writable()
	if !s.Writable {
		s.Note = errReadOnly.Error()
	}
	return s
}

// Apply writes all three key-point tables and the rapid trigger / turbo block, then re-reads identity.
// The keyboard cannot report actuation values back, so the caller must treat them as write-only.
func (k *Keyboard) Apply(req ApplyRequest) ([]Step, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		return nil, errNotConnected
	}
	if !k.writable() {
		return nil, errReadOnly
	}
	p, err := k.model.precisionFor(k.id.Firmware, req.Scale)
	if err != nil {
		return nil, badInput{err}
	}
	if err := checkLastWin(req.LastWin, req.LastWinPairs); err != nil {
		return nil, err
	}
	act, err := toUnits(req.Actuation, p, p.Min, p.Max)
	if err != nil {
		return nil, badInput{fmt.Errorf("actuation: %w", err)}
	}
	down, err := toUnits(req.Downstroke, p, 0, p.Max)
	if err != nil {
		return nil, badInput{fmt.Errorf("downstroke: %w", err)}
	}
	up, err := toUnits(req.Upstroke, p, 0, p.Max)
	if err != nil {
		return nil, badInput{fmt.Errorf("upstroke: %w", err)}
	}

	resume := k.pauseLocked()
	defer resume()

	modes, err := k.modesLocked(req.RapidTrigger, req.Turbo, lastWinMode(req.LastWin == "on", req.RapidTrigger), req.AutoMatch)
	steps := []Step{modes}
	if err != nil {
		return steps, err
	}
	if req.LastWin == "on" {
		lw, err := k.lastWinLocked(req.RapidTrigger, req.LastWinReplace, req.LastWinPairs)
		steps = append(steps, lw)
		if err != nil {
			return steps, err
		}
	}
	for _, w := range []struct {
		name  string
		sub   byte
		units []byte
		pad   byte
	}{
		{"Actuation", subActuation, act, unitsOf(defaultMM, p)},
		{"Downstroke", subDownstroke, down, 0},
		{"Upstroke", subUpstroke, up, 0},
	} {
		acked, err := k.writeKeyPointsLocked(w.sub, w.units, w.pad)
		step := Step{Name: w.name, OK: err == nil}
		switch {
		case err != nil:
			step.Warn = err.Error()
		case !acked:
			step.Warn = "no ack within 500 ms on some packets"
		}
		steps = append(steps, step)
		if err != nil {
			return steps, err
		}
	}
	return append(steps, k.readbackLocked(req.RapidTrigger, req.Turbo)), nil
}

// SetModes writes only the rapid trigger and turbo flags.
func (k *Keyboard) SetModes(rt, turbo bool) ([]Step, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		return nil, errNotConnected
	}
	if !k.writable() {
		return nil, errReadOnly
	}
	resume := k.pauseLocked()
	defer resume()
	modes, err := k.modesLocked(rt, turbo, k.id.LastWin, k.id.AutoMatch != 0)
	if err != nil {
		return []Step{modes}, err
	}
	return []Step{modes, k.readbackLocked(rt, turbo)}, nil
}

// StartLive turns the travel stream on and returns the precision its values are decoded with.
func (k *Keyboard) StartLive() (Precision, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		return Precision{}, errNotConnected
	}
	if err := k.trackingLocked(true); err != nil {
		return Precision{}, err
	}
	k.travelSeen = [keySlots]bool{}
	k.live = true
	p, _ := k.model.precisionFor(k.id.Firmware, "auto")
	return p, nil
}

// PollTravel reads the travel frames that arrive within timeout, then returns the latest value of every
// slot seen since StartLive, not only the slots that reported in this window.
func (k *Keyboard) PollTravel(timeout time.Duration) (map[int]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		return nil, errNotConnected
	}
	for deadline := time.Now().Add(timeout); ; {
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		f, err := k.link.read(rem)
		if err != nil {
			k.dropLocked(err)
			return k.seenTravelLocked(), err
		}
		if pkt, vals, ok := decodeTravel(f); ok {
			for i, v := range vals {
				if slot := pkt*packetSlots + i; slot < keySlots {
					k.travel[slot], k.travelSeen[slot] = v, true
				}
			}
		}
	}
	return k.seenTravelLocked(), nil
}

// seenTravelLocked copies the latest stored value of every slot that has reported this session.
func (k *Keyboard) seenTravelLocked() map[int]byte {
	out := make(map[int]byte, keySlots)
	for slot, seen := range k.travelSeen {
		if seen {
			out[slot] = k.travel[slot]
		}
	}
	return out
}

func (k *Keyboard) StopLive() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.live {
		k.live = false
		_ = k.trackingLocked(false)
	}
}

func (k *Keyboard) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.live {
		_ = k.trackingLocked(false)
	}
	k.closeLinkLocked()
}

func (k *Keyboard) writable() bool {
	return k.link != nil && k.model.layout && !k.model.highPrec
}

func (k *Keyboard) connectLocked() {
	if k.sim {
		k.link = &simLink{start: time.Now()}
		id, err := k.identifyLocked()
		if err != nil {
			k.closeLinkLocked()
			k.lastErr = err.Error()
			return
		}
		k.attachLocked(id, "simulated", "simulated")
		return
	}
	cands := hidCandidates()
	if len(cands) == 0 {
		k.lastErr = "no DrunkDeer keyboard on USB"
		return
	}
	for _, c := range cands {
		dev, err := hid.OpenPath(c.Path)
		if err != nil {
			k.lastErr = err.Error()
			continue
		}
		k.link = &hidLink{dev: dev, logf: k.logf}
		id, err := k.identifyLocked()
		if err != nil {
			k.closeLinkLocked()
			k.lastErr = fmt.Sprintf("%s did not answer: %v", c.Path, err)
			continue
		}
		k.attachLocked(id, c.Path, fmt.Sprintf("page 0xFF00, usage 0x%02X", c.Usage))
		return
	}
}

// hidCandidates lists the DrunkDeer vendor interfaces. Usage 0 is tried first.
func hidCandidates() []hid.DeviceInfo {
	var out []hid.DeviceInfo
	_ = hid.Enumerate(hid.VendorIDAny, hid.ProductIDAny, func(info *hid.DeviceInfo) error {
		ok := false
		for _, p := range ddProducts {
			ok = ok || (p[0] == info.VendorID && p[1] == info.ProductID)
		}
		if ok && info.UsagePage == 0xFF00 {
			out = append(out, *info)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Usage == 0 && out[j].Usage != 0 })
	return out
}

func (k *Keyboard) attachLocked(id Identity, path, iface string) {
	k.id, k.path, k.iface, k.lastErr = id, path, iface, ""
	k.model, k.variant, _ = lookupModel(id.Bytes)
	log.Printf("keyboard %s %x fw %d via %s", k.model.name, id.Bytes, id.Firmware, path)
}

func (k *Keyboard) closeLinkLocked() {
	if k.link != nil {
		k.link.close()
		k.link = nil
	}
	k.live = false
}

func (k *Keyboard) dropLocked(err error) {
	if k.link == nil {
		return
	}
	log.Printf("keyboard lost: %v", err)
	k.closeLinkLocked()
	k.lastErr = err.Error()
}

// drainLocked discards frames that arrived before the next exchange, such as stale travel frames.
func (k *Keyboard) drainLocked() {
	for i := 0; i < 64 && k.link != nil; i++ {
		f, err := k.link.read(time.Millisecond)
		if err != nil || f == nil {
			return
		}
	}
}

// sendLocked writes one frame and, when ack is set, waits up to ackTimeout for a reply it matches.
// A missing ack is reported as acked=false, not an error. A failed write or read drops the link.
func (k *Keyboard) sendLocked(frame []byte, ack func([]byte) bool) (bool, error) {
	return k.sendWaitLocked(frame, ack, ackTimeout)
}

// sendWaitLocked is sendLocked with its own reply timeout. Frames that do not match ack are skipped;
// hidLink.read logs every frame received, so they stay visible in the log.
func (k *Keyboard) sendWaitLocked(frame []byte, ack func([]byte) bool, timeout time.Duration) (bool, error) {
	if k.link == nil {
		return false, errNotConnected
	}
	k.drainLocked()
	if err := k.link.write(frame); err != nil {
		k.dropLocked(err)
		return false, err
	}
	time.Sleep(writeGap)
	if ack == nil {
		return true, nil
	}
	for deadline := time.Now().Add(timeout); ; {
		rem := time.Until(deadline)
		if rem <= 0 {
			return false, nil
		}
		f, err := k.link.read(rem)
		if err != nil {
			k.dropLocked(err)
			return false, err
		}
		if f != nil && ack(f) {
			return true, nil
		}
	}
}

// isHead matches replies that start with b.
func isHead(b byte) func([]byte) bool {
	return func(f []byte) bool { return f[0] == b }
}

// identifyLocked sends A0 02 and parses the reply. Silence yields errNoReply and keeps the link.
func (k *Keyboard) identifyLocked() (Identity, error) {
	if k.link == nil {
		return Identity{}, errNotConnected
	}
	k.drainLocked()
	if err := k.link.write(padFrame(cmdIdentity, 0x02)); err != nil {
		k.dropLocked(err)
		return Identity{}, err
	}
	for deadline := time.Now().Add(ackTimeout); ; {
		rem := time.Until(deadline)
		if rem <= 0 {
			return Identity{}, errNoReply
		}
		f, err := k.link.read(rem)
		if err != nil {
			k.dropLocked(err)
			return Identity{}, err
		}
		if f != nil && f[0] == cmdIdentity && f[1] == 0x02 {
			return parseIdentity(f)
		}
	}
}

func (k *Keyboard) refreshLocked() error {
	id, err := k.identifyLocked()
	if err == nil {
		k.id = id
	}
	return err
}

func (k *Keyboard) trackingLocked(on bool) error {
	_, err := k.sendLocked(padFrame(cmdKeyPoint, subTracking, boolByte(on)), nil)
	return err
}

// pauseLocked stops the travel stream for a write and returns the func that restarts it.
func (k *Keyboard) pauseLocked() func() {
	if !k.live {
		return func() {}
	}
	_ = k.trackingLocked(false)
	return func() {
		if k.live {
			_ = k.trackingLocked(true)
		}
	}
}

// lightingPauseLocked turns travel tracking off before an RGB write, even when the live view is not on,
// so the keyboard is not streaming 0xB7 frames during the write. The returned func turns tracking back on
// only if the live view is still on, so k.live is never changed here.
func (k *Keyboard) lightingPauseLocked() func() {
	_ = k.trackingLocked(false)
	return func() {
		if k.live {
			_ = k.trackingLocked(true)
		}
	}
}

func (k *Keyboard) modesLocked(rt, turbo bool, lastWin byte, autoMatch bool) (Step, error) {
	acked, err := k.sendLocked(commonFrame(turbo, rt, lastWin, autoMatch), isHead(0xB5))
	s := Step{Name: "Rapid trigger / turbo", OK: err == nil}
	switch {
	case err != nil:
		s.Warn = err.Error()
	case !acked:
		s.Warn = "no ack within 500 ms"
	}
	return s, err
}

// lastWinLocked sends the Last Win mode (FC 0A), the replace flag (FC 0B) and the pair table (FC 01).
// The caller has already checked pairs with checkLastWin.
func (k *Keyboard) lastWinLocked(rt, replace bool, pairs [][2]int) (Step, error) {
	s := Step{Name: "Last Win", OK: true}
	all := true
	for _, f := range [][]byte{
		padFrame(cmdLastWin, 0x0A, lastWinMode(true, rt)),
		padFrame(cmdLastWin, 0x0B, boolByte(replace)),
		lastWinPairFrame(pairs),
	} {
		acked, err := k.sendLocked(f, isHead(cmdLastWin))
		if err != nil {
			s.OK, s.Warn = false, err.Error()
			return s, err
		}
		all = all && acked
	}
	if !all {
		s.Warn = "no ack within 500 ms on some packets"
	}
	return s, nil
}

func (k *Keyboard) writeKeyPointsLocked(sub byte, units []byte, pad byte) (bool, error) {
	all := true
	for _, f := range keyPointFrames(sub, units, pad) {
		acked, err := k.sendLocked(f, isHead(cmdKeyPoint))
		if err != nil {
			return false, err
		}
		all = all && acked
	}
	return all, nil
}

// readbackLocked re-reads identity and reports whether the board shows the modes that were written.
func (k *Keyboard) readbackLocked(rt, turbo bool) Step {
	s := Step{Name: "Read-back identity", OK: true}
	if err := k.refreshLocked(); err != nil {
		s.Warn = "could not re-read identity: " + err.Error()
		return s
	}
	if k.id.RapidTrigger != rt || k.id.Turbo != turbo {
		s.Warn = fmt.Sprintf("board reports rapid trigger=%t turbo=%t", k.id.RapidTrigger, k.id.Turbo)
	}
	return s
}
