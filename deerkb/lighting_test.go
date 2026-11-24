package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFindEffect(t *testing.T) {
	eff, ok := findEffect("always")
	if !ok || eff.Code != 0x02 || eff.Kind != "mode" {
		t.Errorf("always: got %+v, %t", eff, ok)
	}
	if _, ok := findEffect("custom"); !ok {
		t.Error("custom: not found")
	}
	for _, id := range []string{"", "nope", "Always"} {
		if _, ok := findEffect(id); ok {
			t.Errorf("findEffect(%q): found", id)
		}
	}
}

// blankColors is 126 valid "#rrggbb" values.
func blankColors() []string {
	c := make([]string, keySlots)
	for i := range c {
		c[i] = "#000000"
	}
	return c
}

func TestParseColors(t *testing.T) {
	rgb, err := parseColors(nil)
	if err != nil || rgb != ([keySlots][3]byte{}) {
		t.Errorf("empty list: err %v, want all black", err)
	}
	c := blankColors()
	c[7] = "#ff8000"
	rgb, err = parseColors(c)
	if err != nil {
		t.Fatal(err)
	}
	if rgb[7] != [3]byte{255, 128, 0} {
		t.Errorf("slot 7: got %v, want [255 128 0]", rgb[7])
	}
	cases := []struct {
		name string
		mut  func([]string) []string
	}{
		{"5 values", func(c []string) []string { return c[:5] }},
		{"no #", func(c []string) []string { c[3] = "ff0000"; return c }},
		{"short hex", func(c []string) []string { c[3] = "#ff00"; return c }},
		{"non-hex digit", func(c []string) []string { c[3] = "#gg0000"; return c }},
		{"8 characters", func(c []string) []string { c[3] = "#1234567"; return c }},
	}
	for _, tc := range cases {
		if _, err := parseColors(tc.mut(blankColors())); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func TestScaleRGB(t *testing.T) {
	var rgb [keySlots][3]byte
	rgb[7] = [3]byte{255, 128, 9}
	if got := scaleRGB(rgb, 0)[7]; got != [3]byte{} {
		t.Errorf("base 0: got %v, want black", got)
	}
	if got := scaleRGB(rgb, 9); got != rgb {
		t.Error("base 9: colours changed")
	}
	// 255*4/9=113, 128*4/9=56, 9*4/9=4
	if got := scaleRGB(rgb, 4)[7]; got != [3]byte{113, 56, 4} {
		t.Errorf("base 4: got %v, want [113 56 4]", got)
	}
}

// Base 0 must reach the wire as black: the custom frame equals one built from all-black colours.
func TestLightingFramesBaseZeroIsBlack(t *testing.T) {
	var rgb [keySlots][3]byte
	for i := range rgb {
		rgb[i] = [3]byte{200, 100, 50}
	}
	eff, _ := findEffect("custom")
	black := lightingFrames(eff, 9, 5, [keySlots][3]byte{})
	if got := lightingFrames(eff, 9, 5, scaleRGB(rgb, 0)); !reflect.DeepEqual(got, black) {
		t.Error("base 0: frames differ from all-black frames")
	}
	if got := lightingFrames(eff, 9, 5, scaleRGB(rgb, 9)); !reflect.DeepEqual(got, lightingFrames(eff, 9, 5, rgb)) {
		t.Error("base 9: frames differ from unscaled frames")
	}
	if reflect.DeepEqual(lightingFrames(eff, 9, 5, scaleRGB(rgb, 4)), lightingFrames(eff, 9, 5, rgb)) {
		t.Error("base 4: frames unchanged")
	}
}

func TestSpeedToFirmware(t *testing.T) {
	if got := speedToFirmware(0); got != 9 {
		t.Errorf("user 0: got %d, want 9 (slowest)", got)
	}
	if got := speedToFirmware(9); got != 1 {
		t.Errorf("user 9: got %d, want 1 (fastest)", got)
	}
	prev := byte(255)
	for u := -3; u <= 12; u++ {
		got := speedToFirmware(u)
		if got == 0 {
			t.Errorf("user %d: firmware 0 would read as off", u)
		}
		if u >= 0 && u <= 9 && got > prev {
			t.Errorf("user %d: firmware %d rises above %d", u, got, prev)
		}
		if u >= 0 && u <= 9 {
			prev = got
		}
	}
	if speedToFirmware(-3) != 9 || speedToFirmware(12) != 1 {
		t.Error("out-of-range speed not clamped to 0-9")
	}
}

func TestLightingFramesModeSpeedIndex(t *testing.T) {
	eff, _ := findEffect("always")
	f := lightingFrames(eff, 9, 0, [keySlots][3]byte{})
	if len(f) != 1 || f[0][6] != 9 {
		t.Errorf("mode speed 0: got frame %v, want speed byte 9 at index 6", f)
	}
}

// tintedModeTest checks every frame is an RgbKeyDataPacket with the mode code, and that every LED entry is the tint.
func TestTintedModeFrames(t *testing.T) {
	eff, _ := findEffect("ripple")
	tint, on, err := parseTint("#8000ff")
	if err != nil || !on {
		t.Fatalf("parseTint: on %t, err %v", on, err)
	}
	frames := tintedFrames(eff, 9, tint, 9)
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	entries := 0
	for n, f := range frames {
		if f[0] != cmdLight || f[1] != 0x01 {
			t.Errorf("frame %d: header % x, want AE 01", n, f[:2])
		}
		if f[4] != eff.Code {
			t.Errorf("frame %d: mode_index %#x, want %#x", n, f[4], eff.Code)
		}
		if f[5] != 0x06 {
			t.Errorf("frame %d: index 5 is %#x, want 0x06", n, f[5])
		}
		for off := 8; off+3 < len(f) && f[off] != 0xFF; off += 4 {
			entries++
			if f[off+1] != 0x80 || f[off+2] != 0x00 || f[off+3] != 0xFF {
				t.Errorf("frame %d entry at %d: colour % x, want 80 00 ff", n, off, f[off+1:off+4])
			}
		}
	}
	if entries != len(isoLEDs) {
		t.Errorf("tinted entries: got %d, want %d", entries, len(isoLEDs))
	}
}

func TestUntintedModeIsSingleSetFrame(t *testing.T) {
	eff, _ := findEffect("ripple")
	if _, on, _ := parseTint(""); on {
		t.Error("empty tint: reported as set")
	}
	f := lightingFrames(eff, 9, 5, [keySlots][3]byte{})
	if len(f) != 1 || f[0][0] != cmdLight || f[0][1] != 0x01 || f[0][4] != eff.Code {
		t.Errorf("untinted mode: got %v, want one SetLightingMode frame with code %#x", f, eff.Code)
	}
}

func TestParseTintRejectsBadInput(t *testing.T) {
	for _, s := range []string{"8000ff", "#8000f", "#80000ff", "#gg0000", "#+80000", "purple"} {
		if _, on, err := parseTint(s); err == nil || on {
			t.Errorf("tint %q: accepted", s)
		}
	}
}

func TestLightBaseJSONDefault(t *testing.T) {
	var ls LightSettings
	if err := json.Unmarshal([]byte(`{"effect":"custom","brightness":9,"speed":5}`), &ls); err != nil {
		t.Fatal(err)
	}
	if ls.Base != 9 {
		t.Errorf("no base key: got Base %d, want 9", ls.Base)
	}
	var zero LightSettings
	if err := json.Unmarshal([]byte(`{"effect":"custom","base":0}`), &zero); err != nil {
		t.Fatal(err)
	}
	if zero.Base != 0 {
		t.Errorf("explicit base 0: got %d, want 0", zero.Base)
	}
}
