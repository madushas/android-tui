# Changelog

## Unreleased
### Engineering
- CI (GitHub Actions): gofmt, vet, tests on Linux + macOS, race detector, golangci-lint, TUI smoke test, cross-compile matrix, govulncheck; Dependabot for Go modules and actions
- Release pipeline: pushing a `v*` tag builds Linux/macOS archives + checksums with GoReleaser; `core.Version` is injected at build time (`make build` uses `git describe`)
- Makefile: `install`/`dist` fixed for the `bin/` output dir; new `lint`, `fmt-check`, `ci`
- golangci-lint clean-up (39 findings). Real fixes among them: QR-pairing password now comes from `crypto/rand` and is never predictable; `Download` checks write/close errors and removes partial files; zip extraction checks close errors; HTTP User-Agent and SDK hint no longer say `emu`
### Phase 2 (in progress)
- Modes (Device / Optimize / Inspect + Emulator|Connect) with ≤5 tabs each; `{ }` mode, `[ ]` tab; mouse on both rows
- Ctrl-Z undoes the last change group on the selected device
- Overview "Next steps" dashboard with one-key shortcuts (b/o/j/z)
- Action palette: fuzzy matching + frecency; first-run quick-start overlay
- Typed confirmation (`REVERT` / `WIPE`) for revert-all and factory reset
- Narrow terminals (<90 cols) show one column at a time; breadcrumb in the main footer
- Missing adb: startup toast + install hints
- Dashboard: safe tweaks not applied, wireless-debugging hint, SDK updates
- `Z` zooms the main panel; clear per-state guidance when a target is stopped/offline/unauthorized/not selected
- `THEME=default|contrast|mono` (also `ANDROID_TUI_THEME`, `NO_COLOR`)
- getprop existence checks cached for 60s (faster Tweaks reloads)
### Phase 1
- Module path `github.com/madushas/android-tui`; `emu` alias dropped
- Packs: google, vivo, oppo (debloat); brand families vivo/iqoo, oppo/realme/oneplus
- Overview: device profile + revertible-change count + Shizuku hint
- Logcat: preset picker (`P`) and regex match highlighting
- Journal: Apps (disable/enable/system remove, bucket, doze, appops) and System settings edits are now revertible
- Auto "baseline" settings snapshot before the first change per device
- Tweaks tab loads with batched adb reads
- Palette: Start Shizuku (adb)
- `make smoke`: pty smoke test of the TUI against the fake phone (8 scenarios)
- Apache-2.0 license; cross-process journal lock; `shot --stdout|-o`, `uidump -o`
- Rename cleanup: banner, config hint, shot dir, device temp file names
- Nothing OS detection via `ro.build.nothing.version`; Nothing OS 4.1 OTA fixture; `scripts/dev/ota-pkgnames.sh`
