# Orchestrator Board desktop release architecture
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


This fork uses [grzegorzrzeznikiewicz/orchestrator-board](https://github.com/grzegorzrzeznikiewicz/orchestrator-board) as its only product release destination. Product identity is recorded in `frontend/product.json`. The upstream private release conductor is not a publisher for this fork and must not be used as a fallback.

The current approved scope is an unsigned DMG for personal use, installed and replaced manually, with files suitable for a separately authorized upload to this fork’s GitHub Releases. The user confirms that Apple configuration does not exist yet. Apple accounts, certificates, signing, notarization and signed automatic updates are future work outside current OB-10 acceptance. An unsigned DMG has already been built and inspected locally; no release upload is performed by this task.

## Hard rule: exactly one publisher

One explicitly designated publisher owns artifact selection, verification and publication for a release. A future signed release also requires its designated signing conductor. Concurrent publishers must never create divergent artifacts with the same version. The upstream July incident motivating this rule remains applicable to the fork; retaining the rule does not authorize using the upstream conductor or its credentials.

The unsigned build workflow stores artifacts and checksums; it does not create a release or authorize publication. Uploading the selected DMG to GitHub requires a separately authorized operation. Do not use `npm run publish` for the manual draft/prerelease procedure: the existing Forge publisher is configured with `draft: false`.

## Current personal-use manual flow

1. Pin the source SHA, product version, architecture and fork repository.
2. Run `npm run make` from `frontend` with `AO_RELEASE_REPO=grzegorzrzeznikiewicz/orchestrator-board`. The current Forge configuration writes the app and installers under `frontend/out`; after building, copy the completed app, ZIP and DMG into a task-owned artifact directory and record their hashes. `ELECTRON_FORGE_OUT_DIR` does not select the output directory in the installed Forge version. Leave Apple signing/notarization variables unset; a partial credential configuration remains an error. `npm run package` produces the app alone, while `make` adds ZIP and DMG.
3. Inspect the DMG/app identity, icons, bundled LICENSE/NOTICE/dependency texts and baked update destination. Record the exact DMG SHA-256 and verify mounted app bytes against the built app. Retain the existing local artifact evidence and data-preservation checks.
4. For a separately authorized GitHub upload, use an explicitly labeled unsigned manual-install draft or prerelease and provide the DMG architecture/version and SHA-256. Do not designate it latest or publish updater feed files for this unsigned manual route. This task does not upload or publish it.
5. Install or replace the app manually from the DMG, preserving the existing fork data namespace and original application. A downloaded unsigned app may be blocked by macOS Gatekeeper; this route does not claim Apple trust, notarization or automatic installation. Do not disable OS protections or weaken updater verification.

The current stable-release guard requires all three platform feeds (`latest.yml`, `latest-mac.yml`, `latest-linux.yml`) for a stable latest release. A DMG-only stable release does not satisfy that contract. Keep that guard unchanged; a future stable distribution policy needs its own decision.

For an installation that deliberately uses a separate data directory, the optional fork-only `~/.ao/orchestrator-board/desktop-launch.json` preserves that selection for ordinary Finder/Dock starts. Its version 1 object requires `version`, `dataDir`, `runFile`, and `port`; paths must be absolute and inside `~/.ao`. Follow [the data selection contract](../../docs/ob-10-fork-identity.md#data-and-coexistence). Do not ship a machine-specific file, change global launch environment, or merge the original application's data. Acceptance must include a full quit and ordinary launch with the project, conversation draft, theme and daemon configuration preserved. A restart through the same environment-configured shell alone does not establish this.

## Future signed release flow

1. Pin the source SHA, product version, channel, architectures and fork repository.
2. Build artifacts and verify the recorded SHA-256 inventory. Inspect product identity, icons, SPDX metadata, bundled notices and `app-update.yml` before signing.
3. Sign macOS applications with the designated Developer ID Application identity, notarize/staple and verify the result. Preserve the existing architecture-dependent Node entitlements.
4. Generate complete channel feeds and ZIP payload metadata, retaining SHA-512 and size information. Do not enable macOS differential downloads.
5. Upload a complete draft to the fork destination; re-download and verify exact draft bytes and feed provenance.
6. Perform the native baseline-to-candidate update with settings/project/data sentinels, then verify relaunched version and daemon health. Also verify wrong/missing feed and invalid payload failure behavior.
7. Publish only when all required checks pass and the authorized conductor is ready. This document does not perform or authorize that publication.

## Public unsigned build boundary

`.github/workflows/build-artifacts.yml` builds a pinned ref/version without signing or publishing. Its generated feed coordinates must equal this fork. A malformed or foreign repository variable is an error. The macOS jobs retain both the existing ZIP and a separate `mac-arm64-dmg` or `mac-x64-dmg` artifact; each DMG SHA-256 is added to its existing platform digest fragment and merged into `digests.json`. Artifacts expire after one day. These unsigned DMGs can support the personal-use manual flow above; they are not signed downloads or GitHub Release assets. The workflow still needs its existing WorkOS build variable and has no release-write permission.

For a local isolated development package, run the normal `frontend` package command with `AO_RELEASE_REPO=grzegorzrzeznikiewicz/orchestrator-board`. Forge writes under `frontend/out`; copy the completed app into a task-owned artifact directory. Use `make` when ZIP and DMG are also needed, then copy those completed files as well. Keep runtime data/profile/run files under `~/.ao/dev/<task>`. Do not copy or modify an original installed app, use real project data for a disposable test, or change `HOME` to redirect state.

## Channels

Stable is the default. Nightly and feature previews, when used, remain in the same fork repository. A missing channel is an unavailable feed, not permission to use an upstream/default channel. Release notes, version floors and direct-download metadata must agree on the fork destination.

## macOS artifacts and verification

The build produces both ZIP and DMG. The current personal-use route uses the DMG for manual installation and replacement. Future automatic macOS updates require ZIP and `latest-mac.yml`; a DMG cannot serve as an automatic update payload. Full ZIP downloads remain the supported path; differential updates stay disabled and the dormant v2 keyring is not a new release path.

For a future signed release, verify macOS artifacts with `frontend/scripts/verify-mac-artifact.sh`. The current unsigned artifact is expected to be rejected by this verifier; that result does not prevent producing a clearly labeled manual-install DMG and must not be reported as a trust verification pass. It preserves the seal with `ditto -x -k`, then checks code signatures, Gatekeeper and notarization. Do not substitute plain unzip, suppress a failure, or report an unsigned fixture as accepted by Gatekeeper.

The native update harness must observe native staging, an actual application swap, the relaunched fork version and a healthy daemon. Dependency download completion alone precedes that boundary. A new Team ID cannot be assumed compatible with the original product’s installed update chain; use a separately named fork installation and signed fork baseline.

## Repair installer

Repair uses an already signed/notarized/stapled fork app. It must match this fork’s bundle ID and installation path, preserve data, and never target the original app. A signed Installer identity, where required, is distinct from the Application identity. The repair is not a replacement for the normal ZIP update or its verification.

No repair or automatic stale-copy cleanup may retire an original `Agent Orchestrator.app`. Test path selection, ownership and original-app preservation explicitly.

## Provenance and historical reference

Agent Orchestrator is the upstream project, Copyright 2026 Untrivial. Existing notices and the Apache-2.0 license remain intact. The previous upstream conductor/runbook description is retained in Git history at the pre-OB-10 main revision `f64ed0f6d1ee42f7395b15d5e7b63d6a9451e3e1`; it is historical reference, not the fork release procedure.

See [fork identity and acceptance status](../../docs/ob-10-fork-identity.md) for the data, authentication, dependency-license and native update boundaries.
