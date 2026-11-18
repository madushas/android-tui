# android-tui

[![CI](https://github.com/madushas/android-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/madushas/android-tui/actions/workflows/ci.yml)

Terminal UI (Go + Bubble Tea) for Android emulators **and** real devices: AVD/ROM management, adb connections (QR pairing),
package manager, device profiling, reversible optimisation tweaks with a rollback journal, debloat with snapshots,
process view, screenshot/UI-dump tools, logcat, settings diff. Scriptable CLI included. Formerly `emu` (no alias kept).

```
make build && ./bin/android-tui      # TUI (needs adb; android CLI optional)
make test                            # vet + unit tests
make lint                            # golangci-lint v2 (install it first)
make ci                              # everything CI runs: privacy check, gofmt, vet, tests, race detector, smoke test
make privacy-check                   # run before committing: no device dumps, serials, IPs, logs or notes in the tree
make smoke                           # TUI smoke test in a pty against the fake phone (python3 + pyte)
make demo                            # fake device, no phone needed
sh scripts/collect-device-info.sh    # read-only real-device dump for fixtures
```
Keys: `1/2/3` panes · `l`/→ into the main panel · `{ }` mode · `[ ]` tab · `:`/Ctrl-P palette · Ctrl-Z undo · `Z` zoom · `?` help

## Mouse and keyboard
Everything works with the keyboard, the mouse, or both. Lists move with ↑/↓ (j/k), PgUp/PgDn, Home/End; Enter activates;
`.` or Ctrl-O (or a right-click) opens a context menu on any list, tab or pane — including per-app actions in the package list;
a double-click is Enter; the wheel scrolls (also inside the palette and pickers); confirmation dialogs have clickable buttons.

## Power users: rootAVD
Optionally, android-tui can drive the third-party [rootAVD](https://gitlab.com/newbit/rootAVD) script to root an emulator with
Magisk. It is **not bundled**: point `ROOTAVD` in the config (or `ANDROID_TUI_ROOTAVD`, or `--script`) at your own copy.
android-tui keeps a one-time backup of the stock ramdisk, asks for a typed confirmation (the ramdisk belongs to the system
image, so every AVD using that image is affected), runs the script on your terminal, verifies that the ramdisk changed and can
restore the original: palette › *Root AVD with rootAVD…* / *Restore stock ramdisk*, or `android-tui rootavd <avd> [--status|--restore]`.

## Contributing
Never commit device dumps, serials, IP/MAC addresses, logs, screenshots or personal notes: use reserved example values
(`192.0.2.x`, `TEST-DEVICE-001`) in fixtures and keep real captures in the git-ignored `.local/` directory.
`make hooks` runs the checks before every commit. Domain terms: [CONTEXT.md](CONTEXT.md). Design decisions: [docs/adr](docs/adr). Release routine: [docs/RELEASING.md](docs/RELEASING.md).
`make privacy-check` enforces this and runs in CI. Source files stay under 600 lines and functions under 100 (`make size-check`):
split by responsibility instead of growing a file.

## Releases
Tagging `vX.Y.Z` builds Linux and macOS (amd64/arm64) archives with checksums via GoReleaser (`.github/workflows/release.yml`).
Local equivalent: `make dist`. Windows is not supported (the code uses Unix-only syscalls).

## License
Apache-2.0 — see [LICENSE](LICENSE).
