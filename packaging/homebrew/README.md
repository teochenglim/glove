# Homebrew packaging

The formula ships here (`glove.rb`); the tap repository
(`homebrew-<user>/homebrew-glove`) is a maintainer action — Homebrew
requires a dedicated git repo, which this one cannot be. The live
`brew install` is the outside-tester pass recorded in `RELEASE.md` §5.

## Per-release steps (maintainer)

1. Tag the release (`make release VERSION=x.y.z`) and wait for CI to publish
   the artifacts + `checksums.txt`.
2. Fill the formula's four `sha256` lines from the release:
   `scripts/update-homebrew-sha.sh x.y.z` (fetches `checksums.txt`, rewrites
   `glove.rb`). Run it before copying so the committed formula here never
   carries a stale hash for a tagged version.
3. Copy `glove.rb` into the tap as `Formula/glove.rb`, commit, push.
4. Verify on a real machine: `brew install <user>/glove/glove` then
   `glove doctor`.

The layout contract test (`TestAC_0_9_1_formula_matches_release_layout`,
internal/release) pins the four download URLs to the release workflow's
artifact naming — a workflow rename that breaks the formula fails
`make check`.
