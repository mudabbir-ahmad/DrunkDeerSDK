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
