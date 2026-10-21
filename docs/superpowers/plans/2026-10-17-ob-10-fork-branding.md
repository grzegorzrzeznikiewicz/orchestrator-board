# OB-10 Fork Branding Implementation Plan
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


> **For agentic workers:** Use the documented independent ownership below and superpowers:dispatching-parallel-agents for separate domains; root integrates and requests one fresh whole-branch review. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Provide Orchestrator Board identity, attribution, fork-only updater protections and an unsigned DMG for personal manual use, with actual package/native evidence. Signing/notarization and signed automatic updates are future work outside the revised current acceptance.

**Architecture:** Keep existing Electron/Go boundaries; frontend/product.json supplies shared desktop identity. Narrow edits to existing packaging, UI and update paths; no workflow rewrite.

**Tech Stack:** Go, Electron Forge 7.11.2, electron-updater 6.8.9, existing Vitest/Go/Playwright harnesses, macOS packaging.

**Spec:** docs/superpowers/specs/2026-10-17-ob-10-fork-branding-design.md; Jira OB-10 read 17.10.2026.

## Global Constraints

- Actual checkout, feature/OB-10 from main f64ed0f6d1ee42f7395b15d5e7b63d6a9451e3e1. No extra Git worktrees; preserve OB-4 stash. Latest user-supplied AGENTS.md explicitly forbids commits and push; those integration steps are not executed under the current instruction.
- Exact identity/known fork in frontend/product.json. No upstream default/fallback; no production publishing or weakened integrity/signature check.
- Retain Apache LICENSE and Copyright 2026 Untrivial; source repositories/artwork read-only.
- Preserve ~/.ao, AO_DATA_DIR, existing data and original installation. Bootstrap original app is not an acceptable fork target.
- The user confirms no Apple configuration exists and revises KA5 to an inspected unsigned DMG for personal manual installation/replacement and possible later GitHub sharing. Apple account/certificate setup, signing/notarization and signed automatic updates are future work outside current OB-10 acceptance. Preserve existing updater signatures and OS protections. Future manual sharing uses a clearly unsigned draft/prerelease without latest/feed metadata; no upload is authorized now.

## Review Focus

- Malformed bundled YAML or a prior upstream staged payload reaches a different updater entry point.
- Alternate prerelease channel or arbitrary release-note/floor artifact URL escapes the fork boundary.
- Renamed app/binary/protocol drifts from repair/relocation or overwrites the upstream installation.
- Packaged licenses disappear under the restrictive Vite filter or claim ownership of upstream code.
- Rebrand changes data paths or agent vendor service addresses by broad string replacement.

### Task 1: Desktop UI, artwork and attribution

**Ownership:** frontend/src/main.ts, renderer/ (except update-policy code), frontend/index.html, frontend/package.json/lock, docs branding helper, assets, license resources/generator. Do not edit forge/makers/update modules/backend.
**Interfaces:** Consume frontend/product.json; supply packaged resources directory frontend/resources/licenses and frontend/assets icon files; report exact paths to packaging owner. About must show origin and allow reading full bundled licenses.

- [x] Add failing focused UI/About/branding/license-resource tests for Orchestrator Board and retained origin.
- [x] Run focused tests, implement product names/firm diamond+board SVG/derived icons; add provenance and license resource generator using installed production dependencies (no new framework).
- [x] Keep translations/vendor references intentional; align product metadata SPDX with unchanged root LICENSE and explain inherited mismatch.
- [x] Run affected full focused suites and report evidence/files/limitations; no commits or browser use.

### Task 2: Runtime update provenance and fail-closed behavior

**Ownership:** frontend/src/main/auto-updater*, feature-builds*, desktop-version-floor*, mac-differential-v2 policy constants if needed, new release-source helper and integration harness/tests. Do not edit main.ts, forge, package metadata or backend.
**Interfaces:** Consume frontend/product.json. Export own fork config validation helper if packaging owner needs it; own all updates staging/provenance policy and update entry-point guards. Preserve existing return types/UI state contracts.

- [x] Write and observe failing tests: absent/malformed/foreign configuration, upstream staged artifact, requested channel404, foreign floor/artifact URL, no update and invalid payload.
- [x] Implement exact fork-only validation at every check/download/install path, remove feature/floor upstream fallback, preserve signature/native readiness and full ZIP behavior. Separate fork update staging from unsafe legacy records without moving user/project data.
- [x] Run focused updater/features/floor/staging suites. Add actual installed-dependency loopback integration where useful; retain original requested URL logs and no signature exceptions. Report pinned dependency limits.

### Task 3: Package, launcher and repair identity

**Ownership:** frontend/forge.config.ts/tests, makers/, scripts/ packaging/repair/update helper, backend/internal/cli/start* and bootstrap scripts, stale-app-copies/relocation modules/tests. No main.ts/renderer/package.json/update-policy edits.
**Interfaces:** Consume frontend/product.json; package Task1 license directory and icons before signing. Exact own fork/name in Go bootstrap, parity with metadata; third-party systeminstall untouched.

- [x] Write and observe failing tests for own bundle/executable/feed/repair identity, no upstream downloads/retirement and correct resources.
- [x] Implement baked validated fork coordinates, distinct bundle/cache/installer/executable/protocol, own CLI launcher discovery and helper/repair guards. No malformed build variable fallback.
- [x] Run affected Forge, maker, repair/helper, stale/relocation and CLI tests; report untested external auth constraints to root.

### Task 4: Integrate, verify, document and publish

**Ownership:** root only; docs/report/ledger, actual package/manual/gates, Jira/Confluence. Commit/push/PR steps are not authorized under the latest user instruction.

- [x] Review independent reports/diffs; resolve interface conflicts with concrete tests, inventory old-brand exceptions and data coexistence.
- [x] Build isolated macOS app/ZIP/DMG, inspect resources/config/icons and controlled feeds; correct native-discovered home/Help destinations and inspect the rebuilt compiled code. Check the current corrected candidate and retain ten latest screenshots in manual-links.
- [x] Full pinned local CI and independent review; fix blocking findings with failing-first regressions and rerun affected full suites. Latest frontend: 486 files, 8184 passed, 7 pre-existing skips; renderer 86/86, type/lint checks pass.
- [x] Publish local results and explicit gaps to Jira and Confluence; read back both writes. No Done or signed-release claim.
- [x] Recheck the corrected native candidate's actual home/Help links, About/six license choices/actual LICENSE opening and readable no-release state; verify process restart preserves Dark theme/project. Enable the fork issue tracker and confirm signed-out GitHub login retains the own issue return target; no issue sent. Select the user's later Light preference through existing settings and capture light settings/board screenshots. Mac access no longer blocks verification.
- [x] Publish the latest results through the working Gama Software Atlassian integration and read back the Confluence report with all ten latest images attached and embedded including Light screenshots, plus Jira comment 10675 with echoed full body. Keep OB-10 W toku; publication does not certify a signed update or authorize commit/push.
- [ ] Complete KA6 notification delivery and click verification. DevTools self-XSS protection blocked pasting the test; its code did not execute, so neither delivery nor clicking is evidenced.
- [x] Revised KA5 artifact evidence: build and inspect the unsigned personal-use DMG, record exact hashes, confirm fork identity/legal resources and ordinary restart/data preservation. No Apple trust or signed-update claim.
- [x] Validate the added workflow DMG collect/upload and SHA-256 merge: four fixture tests pass after four expected RED failures; CI selector/gate contracts 67/67, E2E gate contracts 17/17, actionlint 1.7.12 and the full docs build pass. This workflow stores artifacts only; it does not create GitHub Releases.
- [ ] Independently review the revised personal-use distribution scope and DMG workflow changes before integration.
- Future work, outside current acceptance: Apple setup and signed native update between two test builds, preserving data and existing verification.
- [ ] Integration and exact-head remote CI remain unexecuted under the latest explicit no-commit/push instruction. Merge/Done requires the revised current criteria and authorized integration checks; a signed hop is future work.
