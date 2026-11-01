# android-tui

[![CI](https://github.com/madushas/android-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/madushas/android-tui/actions/workflows/ci.yml)

Terminal UI (Go + Bubble Tea) for Android emulators **and** real devices: AVD/ROM management, adb connections (QR pairing),
package manager, device profiling, reversible optimisation tweaks with a rollback journal, debloat with snapshots,
process view, screenshot/UI-dump tools, logcat, settings diff. Scriptable CLI included. Formerly `emu` (no alias kept).

```
make build && ./bin/android-tui      # TUI (needs adb; android CLI optional)
make test                            # vet + unit tests
make lint                            # golangci-lint v2 (install it first)
make ci                              # everything CI runs: gofmt, vet, tests, race detector, smoke test
make smoke                           # TUI smoke test in a pty against the fake phone (python3 + pyte)
make demo                            # fake device, no phone needed
sh scripts/collect-device-info.sh    # read-only real-device dump for fixtures
```
Keys: `1/2/3` panes · `l`/→ into the main panel · `{ }` mode · `[ ]` tab · `:`/Ctrl-P palette · Ctrl-Z undo · `Z` zoom · `?` help

## Releases
Tagging `vX.Y.Z` builds Linux and macOS (amd64/arm64) archives with checksums via GoReleaser (`.github/workflows/release.yml`).
Local equivalent: `make dist`. Windows is not supported (the code uses Unix-only syscalls).

## License
Apache-2.0 — see [LICENSE](LICENSE).
