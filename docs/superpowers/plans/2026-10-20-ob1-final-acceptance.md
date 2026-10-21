# OB-1 final acceptance implementation plan
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->

> **For agentic workers:** Use superpowers:executing-plans for the sequential ticket work and independent read-only audits for documentation, license provenance and the final review. Track each gate with evidence before changing Jira status.

**Goal:** Finish OB-8 and OB-10, verify the complete approved local workflow, and close OB-9 and OB-1 only when their acceptance criteria are met.

**Architecture:** Keep daemon workflow decisions and durable evidence in the existing Go services; the desktop remains their supervisor. Documentation follows the current contracts. Native notification acceptance uses isolated AO data and a real product event path, without creating another Git worktree or interrupting the user's accepted task.

**Tech stack:** Existing Go daemon, SQLite, Electron/React, Markdown, pinned repository test/build tools.

**Spec:** Jira [OB-8](https://gamasoftware.atlassian.net/browse/OB-8), [OB-10](https://gamasoftware.atlassian.net/browse/OB-10), [OB-9](https://gamasoftware.atlassian.net/browse/OB-9), [OB-1](https://gamasoftware.atlassian.net/browse/OB-1), and the user's instruction of 20 October 2026.

## Global constraints

- Work only in the actual OrchestratorBoard checkout. Use `feature/OB-8`, then `feature/OB-10`; preserve inherited uncommitted work and record each ticket's own delta.
- No commit, amendment, merge commit, push, PR, publishing or additional Git worktree.
- Preserve the user's projects, conversations, profile, accepted workflow and existing original application. Store scratch state and evidence under `~/.ao`.
- The approved distribution is a personal unsigned arm64 DMG for manual installation; Apple Developer ID, notarization, App Store and signed auto-update are outside this acceptance.
- Missing/failed/uncertain verification never counts as success. Human acceptance belongs exclusively to the user.
- Fix only evidenced acceptance failures. Do not add new product features or change other projects.

## Review focus

- Bundle version `0.13.0` must not be confused with the actual source ancestry or the initially analysed `v0.13.3`.
- Carrying previous uncommitted work to a ticket branch must preserve all bytes and keep its delta separately reviewable.
- First-run instructions must match actual Local mode, Codex/Claude readiness, project test-command working directory and automatic orchestrator start.
- Notification evidence must include the real sender, banner and click routing to a durable notification target; an invented notification ID is insufficient.
- License notices and update artifacts must retain actual provenance, own fork identity and agreed unsigned limitations without invented attribution or a fallback to upstream.

## Task 1 — OB-8 user guide and reproducible acceptance procedure

**Files:** Create `docs/ob-8-first-run.md`; update the Install links in `README.md` and `docs/README.md`. Reference `docs/automatic-local-workflow.md`, `docs/ob-10-fork-identity.md` and current UI/service sources.

**Consumes:** Current settings labels and daemon Local workflow constraints; source provenance; inspected package and actual user-accepted workflow evidence.

**Produces:** A linear macOS guide covering installation, provider readiness, Local project setup, real test commands, automatic phases, round limits, acceptance, restart and known limitations.

- [x] Save inherited-work before-images and hashes; select `feature/OB-8` without altering those files.
- [x] Write the guide and both index links. Include exact source/bundle provenance and honestly describe project creation starting an orchestrator.
- [x] Check links, render the documentation build and review instructions against the native UI and existing real accepted run. Do not write implementation-mirroring tests for prose.
- [x] Obtain an independent documentation review; record the OB-8 delta and evidence before moving to OB-10.

## Task 2 — OB-10 remaining distribution and native acceptance

**Files:** `docs/fork-branding-assets.md`, `docs/ob-10-fork-identity.md`; only if supported by new provenance evidence, `frontend/scripts/license-overrides/` and generated `frontend/resources/licenses/`. Product behavior changes require a demonstrated failing regression before implementation.

**Consumes:** Completed OB-8 guide; actual package identity/feed/resources; remaining license-source research and KA6 evidence requirements.

**Produces:** Accurate distribution/attribution documentation, complete applicable notices or explicit blocking evidence, and native notification banner/click acceptance.

- [x] Preserve the completed OB-8 delta and select existing `feature/OB-10` at the same HEAD without losing prior changes.
- [x] Correct obsolete Chromium-profile paths and distinguish current results from historical candidates.
- [x] Audit the six recorded provenance gaps against exact official archives; restore the recovered inherited Lazy notice and explicitly preserve unavailable-original limitations. Correct all evidenced supplied-notice omissions, with version guards and raw-byte coverage. This implements KA2 preservation without inventing upstream copyright or certifying legal compliance.
- [x] Exercise the native notification pipeline on owned isolated data: actual OS presentation and response, exact durable notification ID, native UI routing to its session, package sender/icon identity, and restoration of temporary settings. Record that the agent did not inspect banner pixels; the user subsequently confirmed the correct name, icon and controlled session.
- [x] Verify all KA1–KA6 against source and actual package bytes. Rebuild/install only if packaged source/resources change; preserve user state and exact artifact hashes.

## Task 3 — final gates and Jira/Confluence closeout

**Files:** Add `docs/ob-1-final-acceptance.md`; update the guide/identity status with verified final results. Keep runtime logs, images, recordings, snapshots and machine paths under `~/.ao`.

**Consumes:** Completed ticket deltas and final artifact/source manifests. Existing fresh evidence can be reused only where its exact source/package remains unchanged.

**Produces:** One criterion-by-criterion acceptance report, independent final review, updated Jira and Confluence, and closed issues only after every in-scope gate passes.

- [x] Verify automatic correction and repeat test/review, round exhaustion, stale evidence invalidation, provider failure/uncertainty, restart history and exclusive human acceptance using appropriate existing automated and controlled native scenarios.
- [x] Run the affected full suites, type/lint/static checks, documentation build, contract drift and secret/source-preservation checks. Report opt-in/live/platform skips explicitly; do not claim remote CI for uncommitted source.
- [x] Inspect final APP/ZIP/DMG when rebuilt, including runtime, own update source, notices, artwork and installed-byte parity. Record expected unsigned Apple-trust failures separately from the agreed acceptance gates.
- [x] Obtain a fresh independent final review of the source deltas and acceptance evidence. Fix material findings with meaningful regression tests and rerun affected suites.
- [x] Publish the complete report and evidence to Confluence, read it back, then close OB-8 and OB-10 when their criteria pass, OB-9 after final stabilization, and OB-1 last. Add per-issue descriptions/comments with results and explicit scope limits.

## Execution rulings

The user explicitly authorized execution, sequential ticket branches and closeout after passing tests; no additional plan-approval pause is required. Their no-commit/no-worktree instructions override skill defaults. Uncommitted before-images, per-ticket manifests and the acceptance ledger replace commit-based task boundaries. Documentation-only changes require content/build/manual verification, not tests that merely mirror prose.

The KA2 audit separates two different findings: missing supplied notices (a blocking collector defect, corrected and covered by regression tests) and original historical text absent from exact official archives (an explicit upstream provenance limitation). Independent review confirmed this interpretation against the actual Jira wording. No acceptance criterion was waived.

### Execution checkpoint, 20 October 2026

The final build, installation, automated gates and independent source/evidence review passed with the explicit existing skips and combined backend-package reruns described in `docs/ob-1-final-acceptance.md`. A confirmed snapshot/SSE delivery race was fixed with RED/GREEN regressions and a bounded live-delivery history. The new package reached native-show for a durable controlled notification ID.

macOS locked before the remaining native banner/click, About/licenses and visual restart acceptance. These gates are pending, not passed. The isolated fixture was stopped and the new installed app was returned to its saved real data, with exact preservation of projects, stable session fields, messages, decisions, launch settings and the original application. No OS settings were changed during this attempt. Jira OB-8/OB-10/OB-9 remain in Testy and OB-1 remains in progress; Confluence records both completed work and this manual gate. No commits or pushes were made.

### Execution checkpoint, 21 October 2026

Computer Use works after manual unlock. About, six license choices, the full installed NOTICE, and the own-repository link passed native observation. Ordinary startup restored the selected Budget project Kanban, Light appearance, conversation history and the exact editable unsent draft. The test-only draft was removed without sending. Projects, messages and decisions are identical to the pre-trial backup; SQLite integrity is correct. The real Trains Watch Kanban initially showed its explicit post-restart observation block. A later observation confirmed five fresh checks and a new real Claude review, with current readiness and no new human acceptance. The lead agent did not press Start/Resume/Run review/Accept or send a new instruction; the initiating action is not inferred from automatic flags alone.

The current OS baseline is Work Focus ON with an empty allowed-app list and screen-sharing notifications OFF, rather than the previous evening's Sleep Focus. No OS setting was changed. A separate question about a brief Work trial and immediate restoration remains pending. The controlled fixture was stopped without emitting new notifications and the installed app returned to saved real data. Native banner/click and final closeout remain pending. Independent read-only review confirmed the package/product are unchanged, so the full automated gates remain applicable without another run. OB-8 was closed after its independent acceptance; Confluence version 28 was read back with all six media items preserved. OB-10/OB-9/OB-1 remain open.

### Final native acceptance, 21 October 2026

The user subsequently authorized completely disabling Work. CUA confirmed Work OFF, smart activation OFF and the weekday schedule OFF, with the profile retained. Screen-sharing notifications and persistent alerts were enabled only for the controlled trial, then restored to disabled/Temporary. macOS recorded actual presentation at 08:41:08 UTC and a contents response at 08:41:11. The installed app logged native-click for durable ID ntf_4b1ab76f-af8e-4d59-9185-b4d3e5533512; CUA then showed #/sessions/standalone-1 with the exact controlled-session title. The isolated data retained zero projects/messages/model turns/workflow operations; all seven notifications were resolved/read and its process/daemon were stopped.

The original plan expected direct visual inspection of the banner. The available CUA surface selected an unrelated widget, so independent review assessed the alternative combined evidence against the actual Jira KA1/KA6: package identity inspection, actual OS presentation/response and the native destination. These criteria are met without a separate approval of the banner's appearance. No claim is made that the agent saw banner pixels or that a human confirmed appearance. This evidence is substantially beyond native-show; the changed verification method and its visual limitation remain explicit in the report.

Normal macOS launch restored real Trains Watch, Light mode, health on3010 and SQLite integrity, with projects3/sessions12/messages106/decisions3 preserved. Final workflow v65 is ready/current and unaccepted by the agent; five checks, real Claude round9 and the same snapshot are independently verified. Existing warnings and prior failed trials remain disclosed. No new product changes, commits, pushes, publications or worktrees were introduced.

### Formal closeout, 21 October 2026

Jira readback confirmed OB-1 and every child OB-2–OB-10 in Gotowe. After the earlier OB-8 acceptance, OB-10 and OB-9 were closed, then the epic last. Final result comments are OB-8 10727, OB-10 10731, OB-9 10732 and OB-1 10733. Confluence journal 10977281 version 31 was published and read back with all six existing media items preserved; OB-10 page 10944596 version 20 retains all 30 existing media items, and OB-8 guide 15007748 remains version 1. No commits, pushes, release publication or new worktrees were made.

### Subsequent human notification confirmation, 21 October 2026

The user replied to the pending controlled-banner question: “Kliknąłem — właściwa nazwa, ikona i sesja”. This adds human confirmation of the sender name/icon and opening the correct controlled session. Earlier checkpoints describe the evidence available at their own time; their absence of a human appearance confirmation is historical. The agent still makes no claim to having seen banner pixels or captured a banner screenshot. No exact additional click time is inferred, no new notification trial is needed, and product/package/test results remain unchanged.
