# Linux Arm64 Desktop Build Design

## Goal

Add an arm64 Linux desktop build that is verified in PR CI and shipped as a
tagged release asset without changing Linux updater publishing.

## Scope

This change affects:

- `.github/workflows/desktop-artifacts.yml`
- `.github/workflows/desktop-release.yml`
- `docs/desktop-release-setup.md`

It adds a second Linux build entry that runs on `ubuntu-22.04-arm` and produces
an arm64 AppImage in both pull-request CI artifacts and tagged releases.

This change will not:

- add `linux-aarch64` entries to `latest.json`
- change desktop updater behavior

## Design

The existing desktop workflows already drive cross-target selection through
`AGENTSVIEW_TARGET_TRIPLE`. The arm64 Linux build should reuse that same
pattern in both CI and release workflows.

The desktop artifact workflow will gain one new matrix entry with:

- `name: Linux (arm64)`
- `os: ubuntu-22.04-arm`
- `bundle: appimage`
- `target_triple: aarch64-unknown-linux-gnu`
- a distinct uploaded artifact name
- an artifact path under
  `desktop/src-tauri/target/aarch64-unknown-linux-gnu/release/bundle/appimage/`

The desktop release workflow will split Linux builds into an arch matrix:

- `x86_64` on `ubuntu-22.04`
- `arm64` on `ubuntu-22.04-arm`

The arm64 release job should publish only the `.AppImage` release asset. It
should explicitly disable updater artifact generation so release uploads do not
introduce `linux-aarch64` updater tarballs or signatures.

Linux dependency installation should continue to key off `runner.os == 'Linux'`
so the same package install step runs on both x86_64 and arm64 Ubuntu runners.

The build steps should continue to:

- export `TAURI_ENV_TARGET_TRIPLE` from `AGENTSVIEW_TARGET_TRIPLE`
- run `npm run prepare-sidecar`
- invoke `npx tauri build --target "$AGENTSVIEW_TARGET_TRIPLE"` when the target
  env var is set

The release workflow should keep `latest.json` unchanged so Linux auto-update
support remains `linux-x86_64` only.

## Verification

Verification remains limited to workflow-level checks:

- `bash desktop/scripts/test-prepare-sidecar.sh`
- `bash desktop/scripts/test-startup-ui.sh`
- `bash desktop/scripts/test-desktop-workflows.sh`
- YAML parsing for `.github/workflows/desktop-artifacts.yml`
- YAML parsing for `.github/workflows/desktop-release.yml`

Success means:

- PR CI builds `ubuntu-22.04-arm` Linux artifacts
- tagged releases upload a Linux arm64 `.AppImage`
- `latest.json` remains Linux x86_64-only
