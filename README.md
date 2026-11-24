# DeerKB

DeerKB is an offline replacement for the DrunkDeer web driver, for the **DrunkDeer A75 ISO** keyboard. It runs on Windows only. It talks to the keyboard over USB HID and runs its UI in its own window. DeerKB opens no network port and makes no network connections, so no account or internet connection is needed to edit or apply settings.

> **Verify on your own A75 first.** Real-keyboard writes, the ISO key positions and the lighting frame have not yet been checked on hardware. Before you trust DeerKB with your layout, compare what it applies with what the official driver shows, and keep the official driver installed as a fallback. See `deerkb\NOT-YET-SUPPORTED.md` for the full list.

## Running

1. Unzip `DeerKB-<version>.zip` anywhere.
2. Double-click `DeerKB.exe`. It opens in its own window, which needs the Microsoft Edge WebView2 Runtime (included with Windows 10 and 11).
3. If DeerKB is already running, a second launch shows a message and does nothing else.

Options:

| Flag | Meaning |
|---|---|
| `--simulate` | Use a simulated A75 ISO instead of a real keyboard. Nothing is sent to USB. Good for trying the UI. |

Close DeerKB from the app (Quit).

## Where data lives

Everything is in `%APPDATA%\DeerKB\`:

- `profiles\*.json`: one file per saved profile, in the web driver's format.
- `deerkb.log`: the app log. Attach it when reporting a problem.

## Profiles: import and export

- Profiles use the web driver's `Profile1.json` layout (`keys_array` with 126 slots, each with an action point and rapid-trigger downstroke and upstroke in mm; `rtp` kept as raw JSON).
- DeerKB adds a `deerkb` block to exported files for settings the web driver's format has no place for: scale, rapid trigger, turbo, lighting, and the key remap. The web driver ignores that block, so an exported file still loads there, but it loses DeerKB's extra settings.
- An import file may hold one profile or an array of up to 50. Each profile is validated before it is saved: wrong key count, action points outside 0.2 to 3.3 mm, or unknown lighting or scale values are rejected with a message.
- Importing never overwrites a saved profile. A name that is already taken becomes `Name (2)`, `Name (3)` and so on.

## Firmware

DeerKB does not flash firmware itself. It launches DrunkDeer's own updater (`FirmwareUpgrade.exe`, or the older `Ry_Online_Update*.exe`) and releases the USB link to it while it runs. If DeerKB cannot find the updater, browse to it. When the update is finished, click **I've finished the update** in DeerKB so it takes the keyboard back.

## Building

Requirements: Windows, Go 1.26.5 (see `deerkb\go.mod`). The dependency `github.com/sstallion/go-hid` is fetched by Go.

```
powershell -ExecutionPolicy Bypass -File build.ps1
powershell -ExecutionPolicy Bypass -File build.ps1 -SkipTests
```

The script runs `go vet` and `go test`, then builds `dist\DeerKB.exe` without a console window and `dist\DeerKB-<version>.zip` containing the exe, `NOT-YET-SUPPORTED.md` and this README. The version comes from the first line of a `VERSION` file in this folder, or `0.1.0` if there is none.

## Known limits

- **Per-key actuation is write-only.** The keyboard cannot report actuation or rapid-trigger values back. DeerKB shows what it last applied, not what the board holds. A read-back only confirms the rapid trigger and turbo flags.
- **Last Win and Release Dual-Trigger are not supported.** Their settings survive an import and export in the profile's `rtp` block, but DeerKB cannot change them. The vendor says they need beta firmware.
- **Not yet verified on hardware:** real-keyboard writes, the ISO key positions, and the lighting frame. The tests cover the code paths against a simulated keyboard, not the A75 itself.
- Macros, on-keyboard profile slots, knob mapping, report rate and other features behind the vendor's protocol gateway are not implemented.
- Only the A75 family is writable. Other DrunkDeer models are identified and shown read-only. The A75 Ultra, A75 Master and X60 use a high-precision protocol that DeerKB does not write.
- Windows only. English only. No auto-start or tray icon.

`deerkb\NOT-YET-SUPPORTED.md` has the full list with the leads for each item.
