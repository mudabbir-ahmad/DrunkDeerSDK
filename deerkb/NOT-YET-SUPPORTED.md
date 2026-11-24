# DeerKB: features not in the app yet

Status key: ❌ not built · ⚠️ built but unverified on hardware · 🔎 where a lead might come from

## Firmware
| Feature | Status | Notes / leads |
|---|---|---|
| In-app firmware flashing | ❌ | The vendor flashes through a separate program (`DrunkdeerUpgrader` / `FirmwareUpgrade.exe`; older A75 page names `Ry_Online_Update_v1.2.9.exe`, i.e. an OEM "Ry" updater). The flash protocol is undocumented and a wrong implementation can brick the board. 🔎 Capture the updater's USB traffic with Wireshark+USBPcap (the SDK has `DrunkDeer.ProtocolAnalyzer` for exactly this), then reproduce it only after a recovery path is confirmed. |
| Auto-download of the correct firmware | ❌ | Direct `.bin`/zip URLs aren't published (files look like `drunkdeer_A75_us_v0017_20240511.zip` on drunkdeer.com model pages). The app only opens the release-log page and launches the official updater. 🔎 The Antler web driver's JS bundle decides which file to fetch per model; the SDK already cites `index.unmin.js` of drunkdeer.keybord.net.cn, so the URLs/manifest can be read from there. |
| Firmware version "latest?" check | ❌ | No machine-readable version feed found. 🔎 Same web driver bundle. |

## Keyboard features the web driver has
| Feature | Status | Notes / leads |
|---|---|---|
| On-keyboard profile slots (pull/push/copy/switch) | ❌ | SDK has them (`WriteActiveProfile`, func-block gateway `0x55/0x05-0x06`, KeyTrigger `0xA0/0xA1`) but an A75 owner reports **no released A75 firmware answers the gateway** (see SDK `models.yaml`). DeerKB profiles live on the PC only. 🔎 Newer/beta firmware (A75 beta firmware via beta.drunkdeer-antler.com) may enable it; probe with a read-only `0x55` request. |
| Reading actuation / rapid-trigger values back from the board | ❌ | The standard `0xB6` path only echoes writes. The UI shows "last applied by DeerKB". Needs the gateway above. |
| Last Win (SOCD / snap-tap) | ⚠️/❌ | Packets are documented in the SDK (`CreateLwPairs`, `SetLastWinRapidTriggerMode`) but the vendor says it needs **beta RT-plus firmware** (A75/A75 Pro/G75). Values round-trip in profiles' `rtp` block. |
| Release Dual-Trigger (RDT / RTP authority) | ❌ | Packets exist in the SDK (`RTPAuthority`, `RTPAuthorityDownload`, `ClearRtpUpper`, KeyRemap `0xF8` entries) but are the least documented part and need beta firmware. Settings are preserved in the `rtp` block only. |
| Macros, Dynamic Keystroke, Multi-Tap, Toggle keys | ❌ | SDK `MacroAction`, `DynamicKeystrokeEntry`, `MultiTapEntry` + `ReadMacroChunk/WriteMacroChunk` exist, but they also sit behind the gateway. The web driver's Remap tab doesn't expose them for A75 either. |
| Knob (rotary encoder) mapping | ❌ | Not mappable even in the vendor driver ("cannot assign different functionality to the knob"). |
| Fn / Fn2 layer actions beyond plain key remap | ⚠️ | Remap writes all 3 layers as plain HID keys only. |
| Report-rate / debounce / lock-Win / Alt-Tab lock / stability mode | ❌ | Live in the 64-byte func block (SDK `profile_blocks.yaml`) = gateway-gated. |
| Keyboard-side lighting presets saved to board (`SaveLightingToProfile`) | ❌ | Gateway-gated. Live lighting works. |
| A75 Ultra / Master / X60 high-precision (`0xFD`) writes | ⚠️/❌ | Detected and shown; writes only if time allowed (see README). 🔎 SDK `capabilities/high_precision/*.yaml`. |
| Logo / side light zones | n/a | Not on A75 ISO. |

## Lighting
- Speed mapping: 0 is slowest and maps to firmware 9; the firmware direction is not confirmed on hardware.
- Tinted animated effects send the per-key packet with the animation's mode code; whether the firmware animates the tint is not confirmed on hardware.

## App-level
| Feature | Notes |
|---|---|
| Verified ISO key map | Slot positions follow the web driver's A75-UK screenshots and `Profile1.json`; confirm with Keystroke tracking, then tell us any wrong slot. |
| Auto-start / tray icon / run in background | Not built. |
| Installer (MSI / Inno Setup) | Zip with `DeerKB.exe` only. `ref-cli` has a `drunkdeer_setup.iss` template. |
| macOS / Linux builds | Windows only (HID path + Edge app window). |
| Per-game / per-app profile switching | Not built. |
| Languages | English only. |

## Useful projects to mine
- `mudabbir-ahmad/DrunkDeerSDK` (this SDK): protocol YAML, simulator, `ProtocolAnalyzer` for USB captures.
- `2xxn/cli-drunkdeer`: Go reference; its lighting/RT packets (`0xAE`, `0xB5`).
- `calamity-inc/Soup` `AnalogueKeyboard.cpp`: reads DrunkDeer (VID 0x352D) key depth.
- The Antler web driver JS (`beta.drunkdeer-antler.com`, `drunkdeer.keybord.net.cn`): the real source of truth for firmware manifests, Last Win / RDT, and gateway behaviour. Save the page's JS locally and diff against the SDK yaml.
