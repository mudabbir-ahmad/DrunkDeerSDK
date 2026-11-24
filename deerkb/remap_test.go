package main

import "testing"

func TestRemapRoundTrip(t *testing.T) {
	want := defaultRemap()
	got, err := remapFromInts(remapToInts(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Error("remap changed after int round trip")
	}
	if n := len(remapFrames(want)); n != 42 {
		t.Errorf("remapFrames: got %d packets, want 42", n)
	}
}

func TestRemapRejectsBadInput(t *testing.T) {
	layers := remapToInts(defaultRemap())
	if _, err := remapFromInts(layers[:2]); err == nil {
		t.Error("2 layers accepted")
	}
	if _, err := remapFromInts(append(layers, layers[0])); err == nil {
		t.Error("4 layers accepted")
	}
	short := remapToInts(defaultRemap())
	short[1] = short[1][:keySlots-1]
	if _, err := remapFromInts(short); err == nil {
		t.Error("short layer accepted")
	}
	big := remapToInts(defaultRemap())
	big[0][3] = 256
	if _, err := remapFromInts(big); err == nil {
		t.Error("HID code 256 accepted")
	}
}
