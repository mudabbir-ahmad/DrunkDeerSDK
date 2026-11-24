package main

// keyDef is one physical ISO A75 key in 1u units, origin top-left. Every key is 1u high.
// Slot is the firmware index; HID is the USB usage it sends by default (remap defaults).
// Shape is empty for a plain rectangle. "iso-enter" marks the two halves of the ISO Enter: together they form one L.
type keyDef struct {
	Slot   int     `json:"slot"`
	HID    byte    `json:"hid"`
	Legend string  `json:"legend"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	Shape  string  `json:"shape,omitempty"`
}

// isoKeys is the ISO A75 drawing. Slots 55 and 76 are the two halves of one ISO Enter; the UI selects them together.
// Slot 116 (Fn) is shown but not remappable. The rotary knob is not a key and is not listed.
var isoKeys = []keyDef{
	{0, 0x29, "Esc", 0, 0, 1, ""},
	{2, 0x3A, "F1", 1.25, 0, 1, ""},
	{3, 0x3B, "F2", 2.25, 0, 1, ""},
	{4, 0x3C, "F3", 3.25, 0, 1, ""},
	{5, 0x3D, "F4", 4.25, 0, 1, ""},
	{6, 0x3E, "F5", 5.5, 0, 1, ""},
	{7, 0x3F, "F6", 6.5, 0, 1, ""},
	{8, 0x40, "F7", 7.5, 0, 1, ""},
	{9, 0x41, "F8", 8.5, 0, 1, ""},
	{10, 0x42, "F9", 9.75, 0, 1, ""},
	{11, 0x43, "F10", 10.75, 0, 1, ""},
	{12, 0x44, "F11", 11.75, 0, 1, ""},
	{13, 0x45, "F12", 12.75, 0, 1, ""},
	{14, 0x4C, "Del", 14, 0, 1, ""},

	{21, 0x35, "`", 0, 1.25, 1, ""},
	{22, 0x1E, "1", 1, 1.25, 1, ""},
	{23, 0x1F, "2", 2, 1.25, 1, ""},
	{24, 0x20, "3", 3, 1.25, 1, ""},
	{25, 0x21, "4", 4, 1.25, 1, ""},
	{26, 0x22, "5", 5, 1.25, 1, ""},
	{27, 0x23, "6", 6, 1.25, 1, ""},
	{28, 0x24, "7", 7, 1.25, 1, ""},
	{29, 0x25, "8", 8, 1.25, 1, ""},
	{30, 0x26, "9", 9, 1.25, 1, ""},
	{31, 0x27, "0", 10, 1.25, 1, ""},
	{32, 0x2D, "-", 11, 1.25, 1, ""},
	{33, 0x2E, "=", 12, 1.25, 1, ""},
	{34, 0x2A, "Bksp", 13, 1.25, 2, ""},
	{36, 0x4A, "Home", 15.25, 1.25, 1, ""},

	{42, 0x2B, "Tab", 0, 2.25, 1.5, ""},
	{43, 0x14, "Q", 1.5, 2.25, 1, ""},
	{44, 0x1A, "W", 2.5, 2.25, 1, ""},
	{45, 0x08, "E", 3.5, 2.25, 1, ""},
	{46, 0x15, "R", 4.5, 2.25, 1, ""},
	{47, 0x17, "T", 5.5, 2.25, 1, ""},
	{48, 0x1C, "Y", 6.5, 2.25, 1, ""},
	{49, 0x18, "U", 7.5, 2.25, 1, ""},
	{50, 0x0C, "I", 8.5, 2.25, 1, ""},
	{51, 0x12, "O", 9.5, 2.25, 1, ""},
	{52, 0x13, "P", 10.5, 2.25, 1, ""},
	{53, 0x2F, "[", 11.5, 2.25, 1, ""},
	{54, 0x30, "]", 12.5, 2.25, 1, ""},
	{55, 0x28, "Enter", 13.5, 2.25, 1.5, "iso-enter"},
	{57, 0x4B, "PgUp", 15.25, 2.25, 1, ""},

	{63, 0x39, "Caps", 0, 3.25, 1.75, ""},
	{64, 0x04, "A", 1.75, 3.25, 1, ""},
	{65, 0x16, "S", 2.75, 3.25, 1, ""},
	{66, 0x07, "D", 3.75, 3.25, 1, ""},
	{67, 0x09, "F", 4.75, 3.25, 1, ""},
	{68, 0x0A, "G", 5.75, 3.25, 1, ""},
	{69, 0x0B, "H", 6.75, 3.25, 1, ""},
	{70, 0x0D, "J", 7.75, 3.25, 1, ""},
	{71, 0x0E, "K", 8.75, 3.25, 1, ""},
	{72, 0x0F, "L", 9.75, 3.25, 1, ""},
	{73, 0x33, ";", 10.75, 3.25, 1, ""},
	{74, 0x34, "'", 11.75, 3.25, 1, ""},
	{75, 0x32, "~", 12.75, 3.25, 1, ""},
	{76, 0x28, "Enter", 13.75, 3.25, 1.25, "iso-enter"},
	{78, 0x4E, "PgDn", 15.25, 3.25, 1, ""},

	{84, 0xE1, "Shift", 0, 4.25, 1.25, ""},
	{85, 0x64, "\\", 1.25, 4.25, 1, ""},
	{86, 0x1D, "Z", 2.25, 4.25, 1, ""},
	{87, 0x1B, "X", 3.25, 4.25, 1, ""},
	{88, 0x06, "C", 4.25, 4.25, 1, ""},
	{89, 0x19, "V", 5.25, 4.25, 1, ""},
	{90, 0x05, "B", 6.25, 4.25, 1, ""},
	{91, 0x11, "N", 7.25, 4.25, 1, ""},
	{92, 0x10, "M", 8.25, 4.25, 1, ""},
	{93, 0x36, ",", 9.25, 4.25, 1, ""},
	{94, 0x37, ".", 10.25, 4.25, 1, ""},
	{95, 0x38, "/", 11.25, 4.25, 1, ""},
	{97, 0xE5, "Shift", 12.25, 4.25, 1.75, ""},
	{98, 0x52, "Up", 14, 4.5, 1, ""},
	{99, 0x4D, "End", 15.25, 4.25, 1, ""},

	{105, 0xE0, "Ctrl", 0, 5.25, 1.25, ""},
	{106, 0xE3, "Win", 1.25, 5.25, 1.25, ""},
	{107, 0xE2, "Alt", 2.5, 5.25, 1.25, ""},
	{111, 0x2C, "Space", 3.75, 5.25, 6.25, ""},
	{115, 0xE6, "Alt", 10, 5.25, 1, ""},
	{116, 0x00, "Fn", 11, 5.25, 1, ""},
	{117, 0x65, "Menu", 12, 5.25, 1, ""},
	{119, 0x50, "Left", 13, 5.5, 1, ""},
	{120, 0x51, "Down", 14, 5.5, 1, ""},
	{121, 0x4F, "Right", 15, 5.5, 1, ""},
}

// isoEnterPair are the two firmware slots of the ISO Enter, upper then lower. They are one key, so they always hold one
// actuation, downstroke and upstroke value: the upper slot's (see unifyISOEnter).
var isoEnterPair = [2]int{55, 76}

// noRemap are keys shown but not written by the remap table.
var noRemap = map[int]bool{116: true}

// isoLEDs are the slots with an RGB LED, in the order the lighting packets are sent (SDK KeyLayout.RgbA75Iso).
var isoLEDs = []int{
	0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14,
	21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 36,
	42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 57,
	63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 78,
	84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 97, 98, 99,
	105, 106, 107, 111, 115, 116, 117, 119, 120, 121,
}

// keyGroups are the quick-select sets in the Performance tab.
var keyGroups = map[string][]int{
	"wasd":      {44, 64, 65, 66},
	"numerals":  {22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
	"character": {43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 64, 65, 66, 67, 68, 69, 70, 71, 72, 86, 87, 88, 89, 90, 91, 92},
}

// webNames is the web driver's name for each of the 126 slots, in keys_array order. Profiles keep these names.
// Slot names differ from the drawing in places (the web table puts ARR_L at 118, the SDK LEDs put arrows at 119-121);
// only the drawing and the LEDs are used to act on keys.
var webNames = [keySlots]string{
	"ESC", "", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12", "KP7", "KP8", "KP9", "", "", "", "",
	"SWUNG", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "MINUS", "PLUS", "BACK", "KP4", "KP5", "KP6", "", "", "", "",
	"TAB", "Q", "W", "E", "R", "T", "Y", "U", "I", "O", "P", "BRKTS_L", "BRKTS_R", "SLASH_K29", "KP1", "KP2", "KP3", "", "", "", "",
	"CAPS", "A", "S", "D", "F", "G", "H", "J", "K", "L", "COLON", "QOTATN", "EUR_K42", "RETURN", "", "KP0", "KP_DEL", "", "", "", "",
	"SHF_L", "EUR_K45", "Z", "X", "C", "V", "B", "N", "M", "COMMA", "PERIOD", "VIRGUE", "", "SHF_R", "ARR_UP", "", "NUMS", "", "", "", "",
	"CTRL_L", "WIN_L", "ALT_L", "", "", "", "SPACE", "", "", "", "ALT_R", "FN1", "APP", "ARR_L", "ARR_DW", "ARR_R", "CTRL_R", "", "", "", "",
}
