package main

import "fmt"

// Remap writes three layers (Default, Fn, Fn2). Each packet carries 9 slots, so a layer is 14 packets.
// Every write sends all 42 packets: a partial table could leave keys in a mixed state.
// Unverified on hardware: packet numbering per layer and the Fn1 key's default value.
const (
	cmdRemap        = 0xA0
	subRemap        = 0x04
	remapLayers     = 3
	remapPerPacket  = 9
	remapPerLayer   = keySlots / remapPerPacket
	remapTailAt     = 6 + remapPerPacket*6
	remapEntryCmd   = 0xFC
	remapTailMarker = 0xA5
)

// remapTable holds the HID usage each slot sends, per layer (0 = Default, 1 = Fn, 2 = Fn2).
type remapTable [remapLayers][keySlots]byte

// defaultRemap is the identity mapping of the ISO A75 drawing, applied to every layer.
func defaultRemap() remapTable {
	var t remapTable
	for _, k := range isoKeys {
		for l := range t {
			t[l][k.Slot] = k.HID
		}
	}
	return t
}

func remapToInts(t remapTable) [][]int {
	out := make([][]int, remapLayers)
	for l := range t {
		out[l] = make([]int, keySlots)
		for s, v := range t[l] {
			out[l][s] = int(v)
		}
	}
	return out
}

func remapFromInts(layers [][]int) (remapTable, error) {
	var t remapTable
	if len(layers) != remapLayers {
		return t, badInput{fmt.Errorf("remap: want %d layers, got %d", remapLayers, len(layers))}
	}
	for l, row := range layers {
		if len(row) != keySlots {
			return t, badInput{fmt.Errorf("remap layer %d: want %d slots, got %d", l+1, keySlots, len(row))}
		}
		for s, v := range row {
			if v < 0 || v > 0xFF {
				return t, badInput{fmt.Errorf("remap layer %d slot %d: %d is not a HID code", l+1, s, v)}
			}
			t[l][s] = byte(v)
		}
	}
	return t, nil
}

// remapFrames builds the 42 packets. Entry layout: [slot, 0xFC, 0x00, hid, 0, 0]; tail 0xA5 after 9 entries.
func remapFrames(t remapTable) [][]byte {
	var out [][]byte
	for l := range t {
		for p := 0; p < remapPerLayer; p++ {
			f := padFrame(cmdRemap, 0x02, subRemap, byte(p), 0x0E, byte(l+1))
			for e := 0; e < remapPerPacket; e++ {
				slot := p*remapPerPacket + e
				o := 6 + e*6
				f[o], f[o+1], f[o+3] = byte(slot), remapEntryCmd, t[l][slot]
			}
			f[remapTailAt] = remapTailMarker
			out = append(out, f)
		}
	}
	return out
}

func isRemapAck(f []byte) bool { return f[0] == cmdRemap && f[1] == 0x02 && f[2] == subRemap }

// ApplyRemap writes all three layers. The keyboard cannot report the table back.
func (k *Keyboard) ApplyRemap(t remapTable) ([]Step, error) {
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
	acked := true
	for _, f := range remapFrames(t) {
		ok, err := k.sendLocked(f, isRemapAck)
		if err != nil {
			return []Step{{Name: "Key remap", Warn: err.Error()}}, err
		}
		acked = acked && ok
	}
	s := Step{Name: "Key remap (3 layers, 42 packets)", OK: true}
	if !acked {
		s.Warn = "no ack within 500 ms on some packets"
	}
	return []Step{s}, nil
}
