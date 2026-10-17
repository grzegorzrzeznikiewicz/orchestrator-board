# OB-7 Workflow Regression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Prove the existing local workflow across HTTP, SQLite, desktop decisions and real Codex/Claude, with reproducible evidence for every OB-7 criterion.

**Architecture:** Retain the existing daemon/service/UI boundaries. Add regression cases to the existing Go and Vitest harnesses. Run the real Electron checkout with separate task-owned data; test doubles never count as real-model evidence.

**Tech Stack:** Existing Go testing/httptest/SQLite, Vitest/Testing Library and native Electron on macOS; pinned CI toolchains.

**Spec:** [OB-7](https://gamasoftware.atlassian.net/browse/OB-7), acceptance criteria transcribed in `docs/ob-7-regression.md`.

## Global Constraints

- Work in the actual project directory on `feature/OB-7`, based on main `cc3bdffd63cf74291f8ad3bb550cf8beb35ea5a0`; no additional Git worktrees.
- Preserve unrelated OB-4 documentation in its existing stash. One final task commit, push and PR after validation.
- User delegated execution/integration decisions on 2026-10-17; continue autonomously toward the approved epic requirements.
- No new testing framework, network-dependent automated tests, release publishing or production deployment.
- All native app data under `~/.ao/dev/ob7/`. Fixture setup/metadata bootstrap must be disclosed, and restart must actually preserve the live session.
- Historical acceptance must never become current after restart or a changed snapshot. Failed/unavailable checks remain failures/gaps.

## Review Focus

- A timeout or failed shell command must persist a failed test and prevent review, correction and acceptance.
- A malformed or foreign reviewer result must not settle the run or become a valid decision basis.
- Retrying after a committed decision loses its response must not show another dialog or record another decision.
- Switching only the host while a local decision is pending must not leak feedback or late success to the remote panel.
- Restart must preserve operation/decision identity while requiring explicit fresh verification, without silently destroying the native fixture.

### Task 1: Cross-service regression matrix

**Files:** Create `backend/internal/httpd/controllers/local_workflow_regression_test.go`; retain existing `local_workflow_integration_test.go` as the positive correction/acceptance/reopen test.

**Interfaces:** Use `localworkflow.New(Deps)`, `NewTestRunner(dataDir)`, real review engine/service and SQLite behind `httpd.NewRouterWithControl`. A test-only runner wrapper supplies a short `TestPlan.CommandTimeout`; model/desktop doubles remain explicit.

- [x] Add table cases for failed command and real process timeout. Assert HTTP snapshot block, persisted result/log, no review/fix/decision, and no false acceptance after store reopen.
- [x] Add malformed-result and wrong-snapshot submission cases through `/reviews/submit`, then a valid submission; assert invalid input cannot advance, while valid evidence can.
- [x] Add edit-after-ready / edit-after-acceptance checks through the real HTTP read/decision boundary with immutable historical decision IDs.
- [x] Run the new cases and complete affected controller/review/localworkflow/snapshot race suites. Existing-correct behavior is characterization, not a claimed RED→GREEN fix; any actual defect gets a reproducing failing test before repair.

### Task 2: Desktop retry and host ownership

**Files:** Modify `frontend/src/main/local-workflow-decisions.test.ts`, `frontend/src/renderer/components/LocalWorkflowPanel.test.tsx`.

**Interfaces:** Existing `createLocalWorkflowDecisionHost`, `LocalWorkflowPanel(sessionId, hostId)`, query-key ownership and exact decision request identity.

- [x] Exercise a recorded request whose response is lost, then exact retry with a fresh private intent and one durable-record simulation. Assert one dialog and identical request identity.
- [x] Switch only `hostId` during a pending decision; reject/resolve the earlier promise and assert no comment, success, error or enabled remote decision leaks into the new host.
- [x] Run focused tests, full frontend type/lint/unit/smoke checks when selected, and preserve honest renderer-vs-daemon coverage labels.

### Task 3: Real native regression and recovery

**Files:** Create/update `docs/ob-7-regression.md`; task-owned ignored bootstrap/scripts/logs/screenshots only.

**Interfaces:** Actual local HTTP API, authenticated installed Codex/Claude, native confirmation dialog, persisted session and workflow facts.

- [x] Launch current checkout with isolated data/profile/run file. Build a plain Git fixture without an additional worktree; document any necessary metadata bootstrap.
- [x] Define a small sum-number task with explicit finite-number/type criteria and `node --test sum.test.js`. Capture a reproducible faulty local state with passing incomplete tests, then observe actual Claude required findings, Codex correction, fresh successful test/review and native human acceptance.
- [x] Exercise changed files, test failure, limit and restart. Use automated evidence for dangerous/slow malformed-result and timeout cases rather than mislabelling injected results as real model output.
- [x] Restart the task daemon/app, verify the same live session and stored history, explicitly resume to obtain new test/review evidence, and record exact results and limitations. Stop only this task's processes afterward.

### Task 4: Gates, review and publication

**Files:** Verification report plus required PR description counts; ignored evidence ledger/archive.

- [x] Run every locally runnable selected CI job with pinned runtimes, including complete suites; report platform-only gaps and check their real remote jobs.
- [x] One fresh whole-branch review; fix Important/Critical findings with a reproducing test, ledger minor deferrals.
- [ ] Final conventional task commit, push `feature/OB-7`, create task-specific PR, attach it and verify remote required CI for its exact head.
- [ ] Update Jira and Confluence with tests, logs, native evidence, limitations and final status; integrate only after gates pass, then start OB-10 separately.
