# Release Process

Releases follow the same tag-driven pattern as Agent Layer: GitHub Actions builds release assets, publishes a GitHub Release, then opens a PR against `conn-castle/homebrew-tap` to update the Homebrew formula.

## Preconditions

- On `main` and up to date with `origin/main`.
- Clean working tree (`git status --porcelain` is empty).
- All release changes committed.
- `CHANGELOG.md` has an entry for the exact release tag.
- The tap formula uses:
  - `desc "Personal structured vault for searchable knowledge, data, files, and records"`
  - `license "PolyForm-Noncommercial-1.0.0"`
- This repository has the Homebrew tap GitHub App secrets configured:
  - `HOMEBREW_TAP_APP_ID`
  - `HOMEBREW_TAP_PRIVATE_KEY`

## Release commands

```bash
VERSION="vX.Y.Z"

git checkout main
git fetch origin
git pull --ff-only origin main
git status --porcelain

make release-preflight RELEASE_TAG="$VERSION"

git tag -a "$VERSION" -m "$VERSION"
git push origin main
git push origin "$VERSION"
```

Release assets are built by GitHub Actions. Stable release tags only are supported: `vX.Y.Z`.

## GitHub release

1. Tag push triggers `.github/workflows/release.yml`.
2. The workflow validates the stable tag format and runs `make release-preflight RELEASE_TAG=<tag>`.
3. The workflow publishes macOS/Linux platform binaries, `personal-context-<version>.tar.gz` source tarball, and `checksums.txt`.
4. Release notes are extracted from the matching `CHANGELOG.md` section.
5. The workflow opens a PR against `conn-castle/homebrew-tap` to add or update `Formula/personal-context.rb` with the release tarball URL and SHA-256.

## Local release checks

```bash
make release-preflight RELEASE_TAG="vX.Y.Z"
make release-dist PC_VERSION="vX.Y.Z" DIST_DIR=dist
```

The Homebrew formula builds from the source tarball and installs the CLI binary as `pc`.
