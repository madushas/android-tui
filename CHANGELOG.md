# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- **Modes and sub-tabs:** Device, Optimize and Inspect (plus Emulator or Connect depending on the target), at most five tabs each. `{` `}` switch mode, `[` `]` switch tab, and the mouse works on both rows. The main footer shows a breadcrumb.
- **Undo:** `Ctrl-Z` reverts the last change group on the selected device. "Revert all" and factory reset now require typing a confirmation word.
- **Overview dashboard:** device profile, a count of revertible changes, and "Next steps" with one-key shortcuts (storage, battery, Shizuku, unverified changes, safe tweaks, SDK updates, wireless debugging).
- **Action palette:** fuzzy matching that remembers what you use; availability reasons for disabled actions.
- **Safer changes:** every change is journalled with the value it replaced (revertible from the Journal tab), an automatic settings snapshot is taken before the first change on a device, and journal writes are safe across processes.
- **Layout:** `Z` zooms the main panel; narrow terminals (under 90 columns) show one column at a time; `THEME=default|contrast|mono` (also `ANDROID_TUI_THEME` and `NO_COLOR`); a first-run quick-start screen.
- **Guidance when nothing is selected:** per-state help for stopped emulators, unauthorized or offline devices, saved connections and a missing `adb`.
- **Tweaks and packs:** batched reads make the Tweaks tab load faster; new debloat packs for Google, vivo and OPPO/realme/OnePlus; Nothing OS detection; Logcat preset picker (`P`) with match highlighting; a "Start Shizuku" action.
- **CLI:** `shot <dev> [file|-o file|--stdout]` and `uidump <dev> [-o file]`.
- **Project:** source files are kept small and focused (`make size-check`: files under 600 lines, functions under 100); Apache-2.0 license; `make lint|ci|race|smoke|dist`; GitHub Actions CI and a tag-triggered release pipeline (Linux and macOS, amd64 and arm64); Dependabot.

- **Context menus everywhere:** right-click, `.` or Ctrl-O on any list, tab or pane opens a menu of that item's actions (apps, emulators, connections, snapshots, and every tab), always in sync with the keyboard shortcuts.
- **Keyboard, mouse and scroll parity:** double-click activates, the wheel scrolls everywhere (the Overview now scrolls, with ↑/↓/PgUp/PgDn/Home/End), Logcat lines can be picked by click, the action palette and all pickers accept the mouse, and confirmation dialogs have clickable buttons.
- **rootAVD (opt-in, power users):** root an emulator with the third-party rootAVD script (not bundled) via the palette, the emulator menu or `android-tui rootavd`; includes a typed confirmation, a one-time backup of the stock ramdisk, result verification and a restore.

### Changed
- The program, its config directory and its default screenshot folder are named `android-tui` (formerly `emu`; no alias is kept). Build output is `bin/android-tui`.
- `core.Version` is set at build time (`make build` derives it from `git describe`).

### Security
- Local state and device-derived output are private: the journal, snapshots, usage data, saved connections, shell history and the config file are written 0600 inside 0700 directories, and so are screenshots, UI dumps, logcat/prop exports and app lists. Existing directories and files from earlier versions are tightened on start.

### Fixed
- Wireless pairing by QR code reports the real outcome: a failed pairing can no longer look successful because another wireless device is connected.
- The QR code is drawn with the standard four-module quiet zone, black on white, so a dark terminal theme no longer inverts it.
- An adb too old for QR pairing is reported as such (and `doctor` checks the adb version) instead of timing out silently.
- Names supplied by a device or a remote server (package names, the device model used in file names, download names) can no longer write outside the folder you chose. Package names are validated, every other name is reduced to a single safe path component, and the Control tab quotes every value it sends to the device shell.

### Fixed
- A data race between the background host-statistics sampling and the screen renderer (a torn read could show a wrong value or, rarely, crash).
- QR pairing: the one-time password now comes from the system CSPRNG; a failure to get randomness aborts pairing instead of producing a predictable password.
- Downloads: write and close errors are detected, and an incomplete file is removed instead of being left behind with a hash that looks valid.
- Zip extraction reports close errors; the HTTP user-agent and the SDK hint no longer say `emu`.
- `make install` and `make dist` work with the `bin/` output directory.
- Lint findings (`errcheck`, `staticcheck`, `unused`) resolved.
