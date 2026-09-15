# Homebrew packaging

The TUI uses the existing [ZenNotes tap](https://github.com/ZenNotes/homebrew-tap),
alongside the desktop app's cask. Install the `zn` formula on macOS or Linux:

```sh
brew install zennotes/tap/zn
zn tui
```

Upgrade with `brew update && brew upgrade zn`.

## Package and release updates

[`Formula/zn.rb`](Formula/zn.rb) is the source of truth. It downloads the official
GoReleaser archives from `ZenNotes/tui`, with separate SHA-256 checksums for
macOS and Linux on arm64 and amd64. Users do not need Go installed.

As with the desktop app, publishing is a manual step after a stable GitHub
release finishes uploading. The formula is mirrored into `Formula/zn.rb` in
`ZenNotes/homebrew-tap`. The TUI release workflow does not push to the tap.

1. Update the version and all four checksums using Python 3.9+ and an
   authenticated [GitHub CLI](https://cli.github.com/):

   ```sh
   python3 packaging/homebrew/update_formula.py X.Y.Z
   ```

   A leading `v` is also accepted. The updater reads GitHub's asset `digest`
   fields and rejects drafts, prereleases, missing archives and invalid
   checksums before writing the formula.

2. Review the formula change and run the updater tests:

   ```sh
   git diff -- packaging/homebrew/Formula/zn.rb
   python3 -B -m unittest discover -s packaging/homebrew -p 'test_*.py'
   ```

   CI also installs the formula and runs its vault/read smoke test on macOS
   and Linux.

3. Copy the formula to a checkout of the existing tap:

   ```sh
   tap_checkout=/path/to/homebrew-tap
   mkdir -p "$tap_checkout/Formula"
   cp packaging/homebrew/Formula/zn.rb "$tap_checkout/Formula/zn.rb"
   ```

4. After review, commit the packaging change here and commit/push `Formula/zn.rb`
   in the tap. The first publication also adds the TUI install instructions to
   the tap's README. Publishing the tap makes the install command available;
   updating this repository alone does not.

## Existing installations

The desktop app's Homebrew cask installs `ZenNotes.app`; this formula installs
the standalone `zn` executable. If a copy installed by Go, the desktop app or a
manual download comes first on `PATH`, `zn` may still run that copy. Check with
`type -a zn`, or run `"$(brew --prefix)/bin/zn" --version` explicitly.
