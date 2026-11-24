package main

import (
	"testing"
	"time"
)

func flatMM(v float64) []float64 {
	s := make([]float64, keySlots)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestApplyNeedsConnection(t *testing.T) {
	if _, err := NewKeyboard(true, t.Logf).Apply(ApplyRequest{}); err != errNotConnected {
		t.Errorf("Apply before connect: got %v, want errNotConnected", err)
	}
}

func TestCommonFrameAutoMatch(t *testing.T) {
	cases := []struct {
		name string
		rt   bool
		am   bool
		want byte
	}{
		{"rt on, auto match on", true, true, 1},
		{"rt on, auto match off", true, false, 0},
		{"rt off, auto match on is not sent", false, true, 0},
	}
	for _, c := range cases {
		f := commonFrame(false, c.rt, 0, c.am)
		if f[0] != cmdCommon || f[8] != boolByte(c.rt) {
			t.Errorf("%s: header or rt byte wrong: % x", c.name, f[:9])
		}
		if f[11] != c.want {
			t.Errorf("%s: byte 11 = %d, want %d", c.name, f[11], c.want)
		}
	}
}

func TestLastWinFrames(t *testing.T) {
	for _, c := range []struct {
		lw, rt bool
		want   byte
	}{{false, false, 0}, {true, false, 1}, {false, true, 2}, {true, true, 3}} {
		if got := lastWinMode(c.lw, c.rt); got != c.want {
			t.Errorf("lastWinMode(%t, %t) = %d, want %d", c.lw, c.rt, got, c.want)
		}
	}
	f := lastWinPairFrame([][2]int{{16, 26}, {17, 27}})
	want := []byte{0xFC, 0x01, 0x00, 0x02, 16, 26, 0, 0, 17, 27, 0, 0}
	if string(f[:len(want)]) != string(want) {
		t.Errorf("pair frame: % x, want % x", f[:len(want)], want)
	}
}

func TestApplyLastWinSteps(t *testing.T) {
	k := NewKeyboard(true, t.Logf)
	k.Redetect()
	req := ApplyRequest{Actuation: flatMM(1.5), Downstroke: flatMM(1.5), Upstroke: flatMM(1.5), RapidTrigger: true, AutoMatch: true}

	steps, err := k.Apply(req)
	if err != nil || len(steps) != 5 {
		t.Fatalf("Last Win off: %d steps, err %v; want 5", len(steps), err)
	}

	req.LastWin, req.LastWinReplace, req.LastWinPairs = "on", true, [][2]int{{16, 26}}
	steps, err = k.Apply(req)
	if err != nil || len(steps) != 6 || steps[1].Name != "Last Win" {
		t.Fatalf("Last Win on: %d steps, err %v; want 6 with Last Win second", len(steps), err)
	}
	for _, st := range steps {
		if st.Warn != "" {
			t.Errorf("step %q: %s", st.Name, st.Warn)
		}
	}

	req.LastWinPairs = make([][2]int, 15)
	if _, err := k.Apply(req); err == nil {
		t.Error("Apply accepted 15 Last Win pairs")
	}
}

func TestSimulatedKeyboardLifecycle(t *testing.T) {
	k := NewKeyboard(true, t.Logf)
	if k.Status().Connected {
		t.Fatal("connected before Redetect")
	}
	k.Redetect()
	if s := k.Status(); !s.Connected || !s.Simulated {
		t.Fatalf("after Redetect: connected=%t simulated=%t error=%q", s.Connected, s.Simulated, s.Error)
	}

	steps, err := k.Apply(ApplyRequest{Actuation: flatMM(1.5), Downstroke: flatMM(1.5), Upstroke: flatMM(1.5)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(steps) != 5 {
		t.Errorf("Apply steps: got %d, want 5", len(steps))
	}
	for _, st := range steps {
		if st.Warn != "" {
			t.Errorf("step %q: %s", st.Name, st.Warn)
		}
	}

	k.Hold(true)
	if k.Status().Connected {
		t.Error("still connected after Hold(true)")
	}
	k.Hold(false)
	k.Redetect()
	if !k.Status().Connected {
		t.Error("not reconnected after Hold(false) and Redetect")
	}

	k.Close()
	k.Close()
	if k.Status().Connected {
		t.Error("connected after Close")
	}
}

func TestPollTravelKeepsSeenSlots(t *testing.T) {
	k := NewKeyboard(true, t.Logf)
	k.Redetect()
	defer k.Close()
	if _, err := k.StartLive(); err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	defer k.StopLive()

	first, err := k.PollTravel(150 * time.Millisecond)
	if err != nil {
		t.Fatalf("first PollTravel: %v", err)
	}
	second, err := k.PollTravel(150 * time.Millisecond)
	if err != nil {
		t.Fatalf("second PollTravel: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("first PollTravel returned no slots from the simulated stream")
	}
	if len(second) < len(first) {
		t.Errorf("second PollTravel returned %d slots, fewer than the first's %d", len(second), len(first))
	}
	for slot := range second {
		if slot < 0 || slot >= keySlots {
			t.Errorf("slot %d out of range", slot)
		}
	}
}

func TestStartLiveClearsSeenSlots(t *testing.T) {
	k := NewKeyboard(true, t.Logf)
	k.Redetect()
	defer k.Close()
	if _, err := k.StartLive(); err != nil {
		t.Fatalf("StartLive: %v", err)
	}
	if _, err := k.PollTravel(150 * time.Millisecond); err != nil {
		t.Fatalf("PollTravel: %v", err)
	}
	k.StopLive()

	if _, err := k.StartLive(); err != nil {
		t.Fatalf("second StartLive: %v", err)
	}
	defer k.StopLive()
	for slot, seen := range k.travelSeen {
		if seen {
			t.Fatalf("slot %d still marked seen after StartLive", slot)
		}
	}
	if got := k.seenTravelLocked(); len(got) != 0 {
		t.Errorf("seenTravelLocked after restart: %d slots, want 0", len(got))
	}
}
