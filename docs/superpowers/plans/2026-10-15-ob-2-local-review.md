# OB 2 Local review Implementation Plan
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


> Execution: superpowers:executing-plans, directly in the actual project directory. User workspace rules prohibit additional worktrees, commits and pushes.

**Goal:** Meet every acceptance criterion of OB-2 while preserving PR review.

**Architecture:** Extend project configuration and existing review runs with a local target. A Go snapshot component hashes actual repository contents, materializes a stable review copy and observes mutations. The review engine owns launch gates and freshness, SQLite persists facts, and the existing API and React UI expose them.

**Tech Stack:** Existing Go daemon, SQLite/sqlc, Electron/React, generated OpenAPI and TypeScript types.

**Spec:** ../specs/2026-10-15-ob-2-local-review-design.md

## Global constraints

- Upstream v0.14.2, branch feature/OB-2, no new commits or pushes.
- Preserve local AGENTS.md and upstream attribution; do not change sibling projects.
- No worktrees. Test and app data stay separate from the user's production database.
- Keep PR review the default and preserve its existing API behavior.
- No automatic user acceptance; a missing or failed result never counts as success.

## Review focus

- Same-size content changes must invalidate review, including untracked and binary files.
- A change reverted during review must not revive that review.
- Unknown worker or observation state must block success.
- Symlinks and nested repositories must not silently escape or truncate the snapshot.
- Restart must preserve history/configuration and reject unverifiable in-flight results.

## Task 1 Configuration and snapshot identity

Files: domain/projectconfig.go, new domain/local_review.go, new internal/localsnapshot package and tests.

- [x] Write and run failing tests for config defaults/validation and Git content fingerprints.
- [x] Add `ReviewMode` and `TestCommands` to typed config; use existing Worker and Reviewers for Codex/Claude.
- [x] Implement content manifest and stable snapshot preparation, excluding ignored untracked files and detecting unstable reads.
- [x] Verify regular, staged, untracked, deleted, renamed, binary, symlink, ignored and same-size edits.

## Task 2 Durable local review facts

Files: new migration 0199, queries/review.sql, generated sqlc files, store/review_store.go and storage tests.

- [x] Write failing tests for local target round trip, invalidation, reopen and migration of legacy PR history.
- [x] Extend ReviewRun with target kind and local snapshot facts without overloading TargetSHA.
- [x] Regenerate sqlc and implement invalidation as durable facts with normal CDC.
- [x] Run migration and storage suites.

## Task 3 Review engine and API

Files: review/local_review.go and tests, existing review trigger/list/submit boundaries, launcher/prompt, service and HTTP DTO/spec generation.

- [x] Write failing tests for launch without PR, active/unknown worker, stale snapshots, edit/revert, restart and failed launch.
- [x] Route Local review from the existing trigger; launch the configured reviewer against the stable snapshot.
- [x] Validate freshness at submission and reads; preserve stale results only as history.
- [x] Expose local state and snapshot identity; regenerate API and run engine/controller regressions.

## Task 4 Interface and acceptance verification

Files: project settings, session review UI/helpers and tests, generated API types, docs/ob-2-verification.md.

- [x] Write failing behavior tests for settings persistence and review without PR.
- [x] Add mode/test command controls and local review action/freshness display.
- [x] Run frontend tests/typecheck and relevant complete backend suites/build/vet.
- [x] Verify the running interface using isolated application data, perform final independent review and record criterion-by-criterion evidence.

## Execution ledger

- Prepared upstream v0.14.2 in this directory, retaining its existing history and preserving local instructions. No new commits created.
- Added configuration, content manifests, isolated snapshots, durable local runs and CDC invalidation; regenerated sqlc and OpenAPI/TypeScript from sources.
- Added default Codex/Claude selection, project settings and local review history/actions. Legacy PR auto-review policy cannot disable a local manual review.
- Independent review identified permission-hook compatibility, transient empty-directory changes, restart continuity, submission races and worker dispatch/restore paths. Addressed the blocking findings with regression tests. Conservative invalidation of newly created/deleted ignored files is documented.
- Completed HTTP integration using real SQLite and snapshot capture with an external-agent launcher test double. Verified successful submission, idempotent submission, same-size edits and mutations immediately before/after result storage.
- Browser preview used mock workspace navigation and the real settings API backed by an isolated database. Local mode and test commands were saved and reread; the temporary preview bridge was restored afterward. Screenshot: docs/screenshots/ob-2-local-review.jpg.
- Full-suite evidence and remaining scope boundaries are recorded in docs/ob-2-verification.md. No task commits or pushes were created.

## Przygotowanie MR na osobne polecenie użytkownika

Polecenie wypchnięcia zmian i przygotowania MR stanowi zgodę na commit i push zmian OB-2. Repozytorium docelowe to fork `grzegorzrzeznikiewicz/orchestrator-board`, bazą MR jest `main`. Gałąź `feature/OB-2` została przesunięta do bieżącej bazy `df4cabc544dcbe2f0e4db5c8d6132ebb623a4d2b` przed commitem zadania; konflikty dodatków w ośmiu katalogach i18n rozwiązano przez zachowanie wpisów z obu stron. Kontrole PR obejmują ponowne pełne testy, linter i dostępne lokalnie zadania CI, na Go 1.27.1 i Node 24.22.0. Natywne środowiska Windows/Linux są sprawdzane przez CI.
