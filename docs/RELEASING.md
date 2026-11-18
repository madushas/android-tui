# Releasing

1. **Ready?** `make ci` (privacy, size, gofmt, vet, tests, race, smoke) and `make lint` are green; `govulncheck ./...` shows nothing new; CHANGELOG `Unreleased` is accurate.
2. **By hand on a real phone** (the fake cannot show these): wireless pairing by QR scans; apply one tweak and `Ctrl-Z` reverts it on the device; right-click and wheel work in your terminal (and in tmux with `mouse on`).
3. **Dry run:** `goreleaser release --snapshot --clean` → four archives + `checksums.txt`; each contains the binary, LICENSE, README, CHANGELOG, completions.
4. **Tag:** move `Unreleased` to the version in CHANGELOG, then `git tag vX.Y.Z && git push origin vX.Y.Z` (`v0.1.0-rc1`-style tags publish as pre-releases).
5. **Verify the published release:** download one archive, check it against `checksums.txt`, run `android-tui version` (must print the tag), start it against `make demo`.
6. **Roll back:** delete the GitHub release and the tag, then publish a fixed patch version. Nothing is deployed server-side; user data lives in `~/.local/share/android-tui` and the journal format only gains fields.
