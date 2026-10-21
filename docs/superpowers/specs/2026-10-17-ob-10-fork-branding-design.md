# OB-10 — fork identity and updates
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


## Intent and authority

Implement Jira OB-10 inside approved epic OB-1 after merged OB-7 PR9. Product: **Orchestrator Board**. User delegated execution/integration judgment and specifically supplied Gama Software artwork in company-site and db-anonymizer on 17.10.2026. Work in actual checkout on feature/OB-10, no extra Git worktrees, preserve stashes. The latest user-supplied AGENTS.md explicitly forbids commits and push; leave all task changes uncommitted as instructed. Do not publish production releases or configure accounts/certificates.

## Current distribution scope

The user revised the requirement to personal use with an unsigned DMG for manual installation/replacement and possible later GitHub sharing. Revised KA5 covers the inspected DMG, identity/resources, exact hashes and data preservation. Apple accounts, certificates, signing/notarization and signed automatic updates are future work outside this OB-10 acceptance. No publication is authorized now. Future manual sharing uses an explicitly unsigned draft/prerelease, without latest designation or updater feeds; stable-release guard requirements remain unchanged. Do not weaken OS protections or updater signature/integrity checks.

## Product boundary

Use frontend/product.json as canonical desktop product identity: Orchestrator Board, com.gamasoftware.orchestrator-board, orchestrator-board executable/protocol, fork grzegorzrzeznikiewicz/orchestrator-board. Default stable; existing selectable release channels must remain on the same fork. Inventory old labels: replace product-facing strings/assets in desktop/installer/docs; preserve genuine origin/license/vendor service/compatibility identifiers with explicit rationale. Do not globally replace source names, API paths or vendor URLs. No unrelated workflow changes.

Company artwork source: /Users/grzegorzrzeznikiewicz/Programowanie/PHP/Projekty/GamaSoftware/web/wordpress/theme/gama-software/assets/images/gama-software-logo.png (company-site); vector adaptation pattern: /Users/grzegorzrzeznikiewicz/Programowanie/PHP/Projekty/GamaSoftware/db-anonymizer/packaging/icons/db-anonymizer.svg. Retain the three blue diamonds; pair with a board/workflow symbol, using editable SVG and derived packaging assets. Record provenance and owner-provided authorization. Do not modify source repositories.

## License boundary

Root LICENSE Apache-2.0 and Copyright 2026 Untrivial remain byte-identical. Retain existing relevant notices, dependencies and asset attributions. About/Licenses must identify Agent Orchestrator as origin and expose the distributed license texts. Explain inherited MIT package metadata versus root Apache before aligning distributed product metadata; never relicense third-party dependencies. Include modification notice with precise fork scope; verify actual packaged resources, not only repository files.

## Update boundary

All product update discovery/download/install/repair/CLI bootstrap paths must use the known fork; absence, invalid or foreign configuration must fail before networking/install, with a readable state. No upstream fallback. Validate actual baked app-update.yml and all runtime entry points, feature discovery, version floor, cached/staged paths and return-to-channel. Preserve integrity/signature checks, full macOS ZIP plus latest-mac.yml, no differential update enablement. Protect existing cached upstream artifacts from being installed by this fork. Third-party agent/prerequisite systeminstall feeds remain vendor-specific.

## Data and coexistence

Keep daemon data, existing projects and Chromium userData under existing ~/.ao / AO_DATA_DIR rules. No silent data move, destructive cleanup, original-app retirement, or overwritten original app/bundle. New bundle/executable/protocol identity must be consistent across packaging, launcher, repair/relocation and links. Evaluate protocol callbacks against actual auth code; record any compatibility constraint instead of breaking an external service. Existing settings/projects must remain accessible; simultaneous access to the same Chromium profile must be safely prevented or isolated without silently migrating original data.

## Verification

TDD for new behavior: missing/malformed/foreign feed -> zero request; fork 404 -> error/no fallback; valid no-update -> current/no payload; bad SHA-512 -> error/no native staging; foreign artifact URL -> no download/install. Include automatic/manual/feature/return-home entry points, missing prerelease channel, stale artifacts after repository identity change. Use installed electron-updater 6.8.9; latest v27 documentation is not evidence of pinned behavior.

Run complete locally runnable CI suites with pinned Go/Node and remote exact-head required CI. Inspect a built macOS artifact (name, Info.plist, icons, LICENSE/NOTICE, package SPDX, embedded update YAML); run native start/restart/About/help and data coexistence checks with isolated task data. Screenshots must show real app. Two signed test fork builds must complete native staging -> swap -> relaunch with preserved settings/projects/data to certify that future signed-update path; this is outside revised KA5 for the current manual DMG route.

## Known dependencies

Known repository verified public/main; no releases yet. Local codesigning identities: 0 valid. The user confirms no Apple configuration exists yet. This does not block current personal-use unsigned DMG acceptance. The unsigned DMG is already built and inspected; the build workflow retains architecture-specific DMGs and SHA-256 alongside its existing ZIP outputs, without publication. Controlled dependency/feed tests and the unsigned package do not certify a signed native update hop. Keep that future limitation explicit and preserve all signature checks.
