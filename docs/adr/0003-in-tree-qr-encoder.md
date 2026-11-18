# 3. The QR encoder is in-tree, not a dependency
Status: accepted, with mitigations

Wireless-debugging pairing needs a QR code, and the project keeps its dependency set small. `internal/qr` is a minimal encoder (byte mode, level M, versions 1–6). Risk: a subtle encoding bug makes phones silently fail to scan. Mitigations: a golden test pins the symbols (changing them must be deliberate), structural tests are planned, and a real-phone scan belongs to the release checklist. If this proves fragile, replace it with a vetted library rather than extending it.
