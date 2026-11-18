# 5. Context menus are derived from key hints
Status: accepted

A tab without its own `Context` gets a menu generated from its `Hints()`; every item sends the same key the keyboard would. Curated menus (apps, emulators, connections, logcat) use the same mechanism. A menu therefore never offers something the shortcuts cannot do. Hint labels are user-facing text and must read well as menu items.
