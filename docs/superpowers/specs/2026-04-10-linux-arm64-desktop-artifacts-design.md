# Linux Arm64 Desktop Artifacts Design

## Goal

Add an arm64 Linux desktop artifact build to CI without changing the tagged
release workflow or desktop updater publishing.

## Scope

This change only affects `.github/workflows/desktop-artifacts.yml`.

It will add a second Linux build entry that runs on `ubuntu-22.04-arm` and
produces arm64 AppImage artifacts for pull requests and manual artifact runs.

This change will not:

- modify `.github/workflows/desktop-release.yml`
- publish new arm64 Linux release assets
- add `linux-aarch64` entries to `latest.json`
- change desktop updater behavior

## Design

The existing desktop artifact workflow already drives cross-target selection
through `AGENTSVIEW_TARGET_TRIPLE`. The arm64 Linux build should reuse that same
pattern.

The workflow will gain one new matrix entry with:

- `name: Linux (arm64)`
- `os: ubuntu-22.04-arm`
- `bundle: appimage`
- `target_triple: aarch64-unknown-linux-gnu`
- a distinct uploaded artifact name
- an artifact path under
  `desktop/src-tauri/target/aarch64-unknown-linux-gnu/release/bundle/appimage/`

Linux dependency installation should continue to key off `runner.os == 'Linux'`
so the same package install step runs on both x86_64 and arm64 Ubuntu runners.

The build step should continue to:

- export `TAURI_ENV_TARGET_TRIPLE` from `AGENTSVIEW_TARGET_TRIPLE`
- run `npm run prepare-sidecar`
- invoke `npx tauri build --target "$AGENTSVIEW_TARGET_TRIPLE"` when the target
  env var is set

## Verification

Verification remains limited to workflow-level checks:

- `bash desktop/scripts/test-prepare-sidecar.sh`
- `bash desktop/scripts/test-startup-ui.sh`
- YAML parsing for `.github/workflows/desktop-artifacts.yml`

Success means the workflow contains a valid `ubuntu-22.04-arm` Linux matrix
entry with target-specific artifact output and leaves release publishing
unchanged.
