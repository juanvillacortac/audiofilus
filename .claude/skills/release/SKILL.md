---
name: release
description: Publish a new Audiofilus release (tag, GitHub Actions build, PortMaster zip and Linux binaries). Use when the author asks to release, publish a version, or cut a tag.
---

# Release

Releases are built by `.github/workflows/release.yml` when a `v*` tag is pushed. It builds with the `Dockerfile` (vet, tests, `build.sh`) and uploads:

| Asset | What it is |
|---|---|
| `audiofilus-portmaster.zip` | The PortMaster port (`dist/audiofilus.zip`) |
| `audiofilus-linux-amd64.tar.gz` | The desktop binary (UI, service and CLI in one), with licenses |
| `audiofilus-linux-arm64.tar.gz` | The same for arm64 |
| `audiofilus-windows-amd64.zip` | `audiofilus.exe`, with licenses |
| `SHA256SUMS` | Checksums of the above |

## Steps

1. Only release when the author asks. Confirm the version number with them if they didn't give one (semver: `vMAJOR.MINOR.PATCH`).
2. Check the tree is clean and green locally:
   ```sh
   git status --short
   gofmt -l . ; go vet ./... && go test ./...
   docker build --target dist --output /tmp/af-dist .   # optional: same build as CI
   ```
3. If the UI changed visibly, refresh the images first (skill `refresh-media`) and commit them.
4. Tag and push:
   ```sh
   git tag -a vX.Y.Z -m "audiofilus vX.Y.Z"
   git push origin main vX.Y.Z
   ```
5. Watch the run and check the assets:
   ```sh
   gh run list --limit 3
   gh run watch <id>
   gh release view vX.Y.Z
   ```

The website's download button points at `releases/latest`, so it updates by itself.

6. Write the release notes (`gh release edit vX.Y.Z --notes-file ...`), then rebuild the website so the AltStore source (`site/altstore.json`, which carries the notes) picks them up: `gh workflow run pages.yml`. The Pages workflow also runs by itself when the Release workflow finishes, but that is before the notes are edited. (It doesn't run on the `release` event: that run is on the tag, and the github-pages environment only deploys from main.)

The binaries know their version (`-X .../internal/version.Version`, from the tag): installed copies from v0.9.1 on find the new release by themselves (Settings → Update).

## Manual builds

`gh workflow run release.yml` builds without publishing a release; the files are attached to the run as an artifact. Use it to test the workflow.
