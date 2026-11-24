package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
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
	cases := map[int]byte{0: 1, 1: 1, 9: 9, -3: 1, 12: 9}
	for u, want := range cases {
		if got := speedToFirmware(u); got != want {
			t.Errorf("user %d: got %d, want %d", u, got, want)
		}
	}
	prev := byte(0)
	for u := 0; u <= 9; u++ {
		got := speedToFirmware(u)
		if got == 0 {
			t.Errorf("user %d: firmware 0 would read as off", u)
		}
		if got < prev {
			t.Errorf("user %d: firmware %d falls below %d", u, got, prev)
		}
		prev = got
	}
}

func TestLightingFramesModeSpeedIndex(t *testing.T) {
	eff, _ := findEffect("always")
	f := lightingFrames(eff, 3, 9, [keySlots][3]byte{})
	if len(f) != 1 || f[0][5] != 9 || f[0][6] != 3 {
		t.Errorf("mode speed 9 brightness 3: got frame %v, want speed 9 at index 5 and brightness 3 at index 6", f)
	}
}

// A tinted animated mode is sent as the single SetLightingMode frame, identical to the untinted one, with no
// per-key packets, and the step warns that the tint is not applied.
func TestTintedModeSendsSingleSetFrame(t *testing.T) {
	want := lightingFrames(lightEffect{ID: "ripple", Kind: "mode", Code: 0x0A}, 9, 5, [keySlots][3]byte{})
	link := &recordingLink{fakeLink: fakeLink{respond: func([]byte) [][]byte { return nil }}}
	k := &Keyboard{link: link, model: mA75}
	step, err := k.ApplyLighting(LightSettings{Effect: "ripple", Brightness: 9, Speed: 5, Base: 9, Tint: "#8000ff"})
	if err != nil {
		t.Fatalf("err %v, want none", err)
	}
	if !step.OK || step.Warn == "" {
		t.Errorf("step %+v, want OK with the tint warning", step)
	}
	var ae [][]byte
	for _, f := range link.sent {
		if f[0] == cmdLight {
			ae = append(ae, f)
		}
	}
	if len(ae) != 1 || !reflect.DeepEqual(ae[0], want[0]) {
		t.Errorf("0xAE frames %x, want one SetLightingMode frame %x", ae, want)
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

// SDK RgbAcknowledge: header [0xAE], Matches is buf[0] == 0xAE. The link strips the report id, so a reply is 63 bytes.
func TestLightAckMatchesSDKReply(t *testing.T) {
	ack := make([]byte, frameLen)
	ack[0] = 0xAE
	if !isLightAck(ack) {
		t.Error("RgbAcknowledge [AE ...]: not matched")
	}
	for _, f := range [][]byte{
		padFrame(0xB5, 0xAE),
		padFrame(0xB6, 0x03, 0xAE),
		padFrame(0x00, 0xAE),
	} {
		if isLightAck(f) {
			t.Errorf("frame %x matched as an RGB ack", f[:4])
		}
	}
}

// fakeLink answers each written frame with the replies from respond; read returns them in order.
type fakeLink struct {
	respond func(frame []byte) [][]byte
	queue   [][]byte
}

func (l *fakeLink) write(frame []byte) error {
	l.queue = append(l.queue, l.respond(frame)...)
	return nil
}

func (l *fakeLink) read(timeout time.Duration) ([]byte, error) {
	if len(l.queue) == 0 {
		time.Sleep(time.Millisecond)
		return nil, nil
	}
	f := l.queue[0]
	l.queue = l.queue[1:]
	return f, nil
}

func (l *fakeLink) close() {}

// An 0xAE reply after an unrelated frame is matched; an RGB packet with no reply is not acked.
func TestSendLockedLightingAck(t *testing.T) {
	reply := make([]byte, frameLen)
	reply[0] = 0xAE
	frame := padFrame(cmdLight, 0x01, 0x00, 0x00, 0x0A, 0x06, 0x09, 0xFF)

	k := &Keyboard{link: &fakeLink{respond: func([]byte) [][]byte {
		return [][]byte{padFrame(cmdKeyPoint, subTracking, 0x00), reply}
	}}}
	ok, err := k.sendLocked(frame, isLightAck)
	if err != nil || !ok {
		t.Errorf("reply after unrelated frame: ok %t, err %v, want acked", ok, err)
	}

	k = &Keyboard{link: &fakeLink{respond: func([]byte) [][]byte { return nil }}}
	ok, err = k.sendLocked(frame, isLightAck)
	if err != nil || ok {
		t.Errorf("no reply: ok %t, err %v, want not acked and no error", ok, err)
	}
}

// recordingLink is a fakeLink that also keeps every frame written to it, in order.
type recordingLink struct {
	fakeLink
	sent [][]byte
}

func (l *recordingLink) write(frame []byte) error {
	l.sent = append(l.sent, append([]byte(nil), frame...))
	return l.fakeLink.write(frame)
}

// The lighting apply path turns travel tracking off before its first RGB packet, whether or not the live
// view is on. Tracking is turned back on afterwards only when the live view is on, and k.live is unchanged.
func TestApplyLightingTurnsTrackingOffFirst(t *testing.T) {
	ackAE := make([]byte, frameLen)
	ackAE[0] = cmdLight
	respond := func(f []byte) [][]byte {
		if f[0] == cmdLight {
			return [][]byte{ackAE}
		}
		return nil
	}
	for _, live := range []bool{true, false} {
		link := &recordingLink{fakeLink: fakeLink{respond: respond}}
		k := &Keyboard{link: link, model: mA75, live: live}
		step, err := k.ApplyLighting(LightSettings{Effect: "always", Brightness: 9, Speed: 5, Base: 9})
		if err != nil || !step.OK || step.Warn != "" {
			t.Fatalf("live %t: step %+v, err %v", live, step, err)
		}
		want := 2
		if live {
			want = 3
		}
		if len(link.sent) != want {
			t.Fatalf("live %t: %d frames written, want %d", live, len(link.sent), want)
		}
		if got := link.sent[0]; !reflect.DeepEqual(got, padFrame(cmdKeyPoint, subTracking, 0x00)) {
			t.Errorf("live %t: first frame %x, want tracking off", live, got[:4])
		}
		if link.sent[1][0] != cmdLight {
			t.Errorf("live %t: second frame %x, want the 0xAE lighting packet", live, link.sent[1][:4])
		}
		if live {
			if got := link.sent[2]; !reflect.DeepEqual(got, padFrame(cmdKeyPoint, subTracking, 0x01)) {
				t.Errorf("live %t: last frame %x, want tracking back on", live, got[:4])
			}
		}
		if k.live != live {
			t.Errorf("live %t: k.live changed to %t", live, k.live)
		}
	}
}

// A keyboard that never acks an 0xAE packet still gets every packet sent. The step is OK with a warning.
func TestApplyLightingSilentKeyboardWarns(t *testing.T) {
	link := &recordingLink{fakeLink: fakeLink{respond: func([]byte) [][]byte { return nil }}}
	k := &Keyboard{link: link, model: mA75}
	step, err := k.ApplyLighting(LightSettings{Effect: "always", Brightness: 9, Speed: 5, Base: 9})
	if err != nil {
		t.Fatalf("err %v, want none", err)
	}
	if !step.OK || step.Warn == "" {
		t.Errorf("step %+v, want OK with a warning about the missing ack", step)
	}
	ae := 0
	for _, f := range link.sent {
		if f[0] == cmdLight {
			ae++
		}
	}
	if ae != 1 {
		t.Errorf("%d 0xAE packets sent, want 1", ae)
	}
}
