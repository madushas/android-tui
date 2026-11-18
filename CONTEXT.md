# android-tui — domain glossary

Terms as used in the code and the UI. Use these words; the "avoid" words caused confusion.

- **Target** — what the user has selected: an **AVD** (emulator, running or not) or an **adb connection**. Most actions need an *online* target.
- **AVD** — an Android Virtual Device defined on this machine. Its **system image** (and the image's `ramdisk.img`) is shared by every AVD created from it.
- **Connection** — a device known to adb (USB, Wi-Fi, or a *saved* Wi-Fi address that is not connected). Avoid: "phone" for the abstract concept.
- **Pack** — a JSON file of **tweaks** and **debloat entries** for a vendor/brand (or `universal`). Applicability is decided by the device **profile**.
- **Profile** — what android-tui knows about one device: form factor, SoC, API level, brand family, root/Shizuku state.
- **Tweak** — a named set of **ops** (settings / device_config / props / appops …) with an *on*, *off* or preset state. **Op** — one read/write on the device.
- **Change** — one applied op. Every change is recorded in the **journal** with the value it replaced; changes applied together share a **group** and are undone together (`Ctrl-Z`).
- **Journal** — the on-disk record of changes. **Revert** = undo one entry or group; **Revert all** = undo everything for a device.
- **Settings snapshot** — a saved dump of the device's settings, used by the Diff tab to compare "before/after". The automatic one taken before the first change on a device is the **baseline**. Avoid: bare "snapshot".
- **AVD snapshot** — an emulator state snapshot (the left pane "AVD snapshots"). Avoid: bare "snapshot".
- **Debloat snapshot** — the list of packages disabled in one debloat action, so it can be restored.
- **Mode / tab** — the main panel is grouped into modes (Device, Optimize, Inspect, plus Emulator or Connect), each with at most five tabs.
- **Palette** — the fuzzy-searchable list of every action (`:` / Ctrl-P). **Context menu** — the per-item action menu (right-click, `.` or Ctrl-O).
- **rootAVD** — an optional, third-party script (not bundled) that patches an AVD's ramdisk with Magisk. android-tui keeps its own **stock-ramdisk backup** next to the image.
