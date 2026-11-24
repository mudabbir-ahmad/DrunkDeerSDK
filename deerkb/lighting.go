package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// lightEffect is one entry of the Colour tab. Codes are the AE-path mode codes (SDK LightingMode).
// "Turbo mode light" is not offered: no SDK code path confirms its wire format.
type lightEffect struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // off, custom or mode
	Code  byte   `json:"-"`
}

var lightEffects = []lightEffect{
	{"off", "Lighting turn off", "off", 0x00},
	{"custom", "Custom light", "custom", 0x13},
	{"rotate", "Rotate Marquee", "mode", 0x01},
	{"always", "Always light", "mode", 0x02},
	{"spectrum", "Spectrum", "mode", 0x03},
	{"breath", "Breath", "mode", 0x04},
	{"press", "Light by press", "mode", 0x05},
	{"stars", "Stars", "mode", 0x06},
	{"wave", "Wave Spectrum", "mode", 0x07},
	{"center", "Center Surfing", "mode", 0x08},
	{"surfdown", "Surfing down", "mode", 0x09},
	{"ripple", "Ripple", "mode", 0x0A},
	{"fish", "Glowing Fish", "mode", 0x0B},
	{"fountain", "Colorful fountain", "mode", 0x0C},
	{"traffic", "Traffic", "mode", 0x0D},
	{"snake", "Gluttonous Snake", "mode", 0x0E},
	{"repeat", "Repeat Surfing", "mode", 0x0F},
	{"cross", "Surfing Cross", "mode", 0x10},
	{"laser", "Laser Key", "mode", 0x11},
	{"randfountain", "Random Fountain", "mode", 0x12},
}

// LightSettings is the Colour tab state. Colors holds 126 "#rrggbb" values by slot and is used by custom only.
// Tint is an optional "#rrggbb" colour for an animated mode; empty means no tint.
// Base is a software scale (0-9) on the custom colours and the tint, as the CLI's --base-brightness. Missing means 9.
type LightSettings struct {
	Effect     string   `json:"effect"`
	Brightness int      `json:"brightness"`
	Speed      int      `json:"speed"`
	Base       int      `json:"base"`
	Colors     []string `json:"colors,omitempty"`
	Tint       string   `json:"tint,omitempty"`
}

// UnmarshalJSON defaults Base to 9 when the key is absent, so an explicit 0 still means black.
func (ls *LightSettings) UnmarshalJSON(b []byte) error {
	type plain LightSettings
	p := plain{Base: 9}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*ls = LightSettings(p)
	return nil
}

const (
	cmdLight   = 0xAE
	rgbPerPacket = 13
)

// rgbAckTimeout is how long each 0xAE packet waits for its reply. The SDK waits 1000 ms per RGB packet
// (KeyboardSession.SendLightingPackets, SendAndReceive default).
const rgbAckTimeout = 1000 * time.Millisecond

func findEffect(id string) (lightEffect, bool) {
	for _, e := range lightEffects {
		if e.ID == id {
			return e, true
		}
	}
	return lightEffect{}, false
}

// parseColors turns 126 "#rrggbb" values into per-slot RGB. An empty list means all black.
func parseColors(hex []string) ([keySlots][3]byte, error) {
	var rgb [keySlots][3]byte
	if len(hex) == 0 {
		return rgb, nil
	}
	if len(hex) != keySlots {
		return rgb, badInput{fmt.Errorf("colors: want %d values, got %d", keySlots, len(hex))}
	}
	for i, c := range hex {
		v, err := strconv.ParseUint(strings.TrimPrefix(c, "#"), 16, 24)
		if err != nil || len(c) != 7 || c[0] != '#' {
			return rgb, badInput{fmt.Errorf("colors[%d]: %q is not #rrggbb", i, c)}
		}
		rgb[i] = [3]byte{byte(v >> 16), byte(v >> 8), byte(v)}
	}
	return rgb, nil
}

// scaleRGB dims every colour by base/9, rounding down. The firmware has one brightness per frame, so this is done in software.
func scaleRGB(rgb [keySlots][3]byte, base int) [keySlots][3]byte {
	for i := range rgb {
		for j := range rgb[i] {
			rgb[i][j] = byte(int(rgb[i][j]) * base / 9)
		}
	}
	return rgb
}

// speedToFirmware maps the user's speed (0 = slowest, 9 = fastest) straight to the firmware byte, unchanged.
// The result is never 0, so speed cannot read as off: user 0 and user 1 both map to 1, the slowest.
// Input is clamped to 0-9. Not confirmed on hardware.
func speedToFirmware(userSpeed int) byte {
	u := min(max(userSpeed, 0), 9)
	return byte(max(u, 1))
}

// parseTint validates the optional tint. Empty means no tint (on is false).
func parseTint(s string) (rgb [3]byte, on bool, err error) {
	if s == "" {
		return rgb, false, nil
	}
	if len(s) != 7 || s[0] != '#' {
		return rgb, false, badInput{fmt.Errorf("tint: %q is not #rrggbb", s)}
	}
	v, err := strconv.ParseUint(s[1:], 16, 24)
	if err != nil {
		return rgb, false, badInput{fmt.Errorf("tint: %q is not #rrggbb", s)}
	}
	return [3]byte{byte(v >> 16), byte(v >> 8), byte(v)}, true, nil
}

// perKeyFrames builds the RgbKeyDataPacket frames (0xAE 0x01 ... index 4 mode code, index 5 0x06), one colour per LED slot.
// The packet has no speed field, so the speed byte is not sent in these frames.
func perKeyFrames(code, brightness byte, rgb [keySlots][3]byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(isoLEDs); i += rgbPerPacket {
		f := padFrame(cmdLight, 0x01, 0x00, 0x00, code, 0x06, brightness, 0xFF)
		off := 8
		for _, slot := range isoLEDs[i:min(i+rgbPerPacket, len(isoLEDs))] {
			f[off] = byte(slot) | 0x80
			copy(f[off+1:off+4], rgb[slot][:])
			off += 4
		}
		if off < frameLen {
			f[off] = 0xFF
		}
		out = append(out, f)
	}
	return out
}

// tintedFrames builds the frames for an animated mode with one tint on every LED, scaled by base like custom colours.
// The RgbKeyDataPacket has no speed field, so the speed setting is not sent with a tinted mode: speed may not apply to it.
// Whether the firmware animates the tint is not confirmed on hardware.
func tintedFrames(eff lightEffect, brightness byte, tint [3]byte, base int) [][]byte {
	var rgb [keySlots][3]byte
	for _, slot := range isoLEDs {
		rgb[slot] = tint
	}
	return perKeyFrames(eff.Code, brightness, scaleRGB(rgb, base))
}

// lightingFrames builds the AE packets for one effect. The mode frame carries speedToFirmware(speed) at index 5
// and brightness at index 6: the firmware reads them in that order, opposite to the SDK field names.
func lightingFrames(eff lightEffect, brightness, speed byte, rgb [keySlots][3]byte) [][]byte {
	switch eff.Kind {
	case "off":
		return [][]byte{padFrame(cmdLight, 0x01, 0x00, 0x00, 0x05, 0x09)}
	case "mode":
		f := padFrame(cmdLight, 0x01, 0x00, 0x00, eff.Code, speedToFirmware(int(speed)), brightness)
		return [][]byte{f}
	}
	return perKeyFrames(eff.Code, brightness, rgb)
}

func isLightAck(f []byte) bool { return f[0] == cmdLight }

// ApplyLighting sends the effect, or the custom colours, to the keyboard.
// Custom (0x13) sends per-key RgbKeyDataPacket frames with the custom colours. Animated modes send the single
// SetLightingMode frame, the only form this firmware acknowledges, so a tint is kept in the settings but is not
// applied to them: the effect runs with the firmware's own colours and the step carries a warning.
func (k *Keyboard) ApplyLighting(ls LightSettings) (Step, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.link == nil {
		return Step{}, errNotConnected
	}
	if !k.writable() {
		return Step{}, errReadOnly
	}
	eff, ok := findEffect(ls.Effect)
	if !ok {
		return Step{}, badInput{fmt.Errorf("unknown lighting effect %q", ls.Effect)}
	}
	if ls.Brightness < 0 || ls.Brightness > 9 || ls.Speed < 0 || ls.Speed > 9 || ls.Base < 0 || ls.Base > 9 {
		return Step{}, badInput{fmt.Errorf("brightness, speed and base must be 0-9")}
	}
	rgb, err := parseColors(ls.Colors)
	if err != nil {
		return Step{}, err
	}
	rgb = scaleRGB(rgb, ls.Base)
	_, tinted, err := parseTint(ls.Tint)
	if err != nil {
		return Step{}, err
	}
	frames := lightingFrames(eff, byte(ls.Brightness), byte(ls.Speed), rgb)
	resume := k.lightingPauseLocked()
	defer resume()
	missed := 0
	for i, f := range frames {
		ok, err := k.sendWaitLocked(f, isLightAck, rgbAckTimeout)
		if err != nil {
			return Step{Name: "Lighting", Warn: err.Error()}, err
		}
		if !ok {
			missed++
			if k.logf != nil {
				k.logf("Lighting: packet %d of %d: no 0xAE ack within %v", i+1, len(frames), rgbAckTimeout)
			}
		}
	}
	s := Step{Name: "Lighting: " + eff.Label, OK: true}
	if tinted && eff.Kind == "mode" {
		s.Warn = "the tint is not applied to animated effects on this firmware; the effect was sent with the firmware's own colours"
	}
	if missed > 0 {
		miss := fmt.Sprintf("keyboard did not acknowledge %d of %d lighting packets within %d ms; all packets were sent, but the keyboard may not have applied them",
			missed, len(frames), rgbAckTimeout.Milliseconds())
		if s.Warn != "" {
			s.Warn += "; " + miss
		} else {
			s.Warn = miss
		}
		if k.logf != nil {
			k.logf("Lighting: %s (effect %s)", miss, eff.ID)
		}
	}
	return s, nil
}
