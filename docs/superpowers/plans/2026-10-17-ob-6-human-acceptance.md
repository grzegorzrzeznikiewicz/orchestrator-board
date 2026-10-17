# OB-6 Human Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Użytkownik może trwale zaakceptować sprawdzone lokalne zmiany albo odesłać je Codexowi z komentarzem, a agent nie może sam nadać akceptacji.

**Architecture:** Koordynator Go waliduje dowody i atomowo zapisuje decyzję wraz z wersją przebiegu w SQLite. Wąski bridge IPC procesu main Electrona potwierdza intencję w natywnym dialogu przez istniejący prywatny broker. Panel Reviews i Kanban wyświetlają wynik wyliczony przez demona.

**Tech Stack:** Go 1.27.1, SQLite/sqlc, Electron/TypeScript, React, istniejące testy Go/Vitest/Playwright i generowany kontrakt OpenAPI.

**Spec:** [2026-10-17-ob-6-human-acceptance-design.md](../specs/2026-10-17-ob-6-human-acceptance-design.md), zatwierdzona 2026-10-17.

Status planu: zatwierdzony przez użytkownika; implementacja i niezależny review zakończone; końcowe bramki publikacji w toku 2026-10-17. Baza `3324f32`, branch `feature/OB-6`. Metoda: native, w bieżącej sesji, zgodnie z zatwierdzoną specyfikacją.

## Global Constraints

- Praca wyłącznie w rzeczywistym katalogu projektu i na `feature/OB-6`; bez dodatkowych worktree.
- Istniejące zmiany OB-4 pozostają w odrębnym stashu i `.cache/ob1-handoff/preexisting-ob4.patch`; nie dołączamy ich do commita OB-6.
- Końcowy commit/push/PR są autoryzowane przez użytkownika. Wykonujemy je po odbiorze OB-6, przed rozpoczęciem następnego zadania. Nie scalamy automatycznie PR.
- Główny listener pozostaje `127.0.0.1`, bez dodawania auth. Nie dodajemy listenera ani akceptacji mobilnej.
- Akceptacja w aplikacji nie wykonuje commit, push, merge ani deploy.
- Intencja: 32 losowe bajty, ważność 60 sekund, tylko pamięć main. Komentarz: najwyżej 4000 znaków Unicode, bez NUL; odesłanie wymaga niepustej treści.
- Review `approved` daje wyłącznie oczekiwanie na człowieka. Accepted wymaga osobnej aktualnej decyzji dla konkretnych testów, review i snapshotu.
- Po utracie obserwacji historia pozostaje, a aktualność wymaga nowych testów/review i nowej decyzji. Limit rund nie zeruje się ani nie rośnie automatycznie.
- Nowa migracja `0202`; bez edycji scalonych migracji i ręcznych zmian sqlc/OpenAPI/typów API. CDC pozostaje oparte na triggerach.
- Zewnętrzne procesy i dane manualnego odbioru są izolowane pod `~/.ao/dev/ob6/`. Nie dotykamy rzeczywistej bazy użytkownika.
- Każdy krok weryfikacyjny ma rzeczywisty odczyt wyniku. Brak platformy/połączenia nie jest wynikiem pozytywnym.

## Review Focus

1. Retry po utracie odpowiedzi po zapisie: jeden rekord i jedna poprawka, bez akceptacji nowszego stanu — testy w zadaniach 1, 2 i 3.
2. Zmiana plików lub Stop podczas natywnego dialogu: potwierdzenie nie może zatwierdzić starego dowodu — zadania 2 i 3.
3. Wykonana wcześniej poprawka i kolejne odesłanie użytkownika: nowy komentarz nie może zostać pominięty ani wysłany dwukrotnie — zadanie 2.
4. Fałszywa intencja, podramka, obcy host lub utrata okna/połączenia: bez zapisu i bez odtworzenia zgody — zadania 3 i 4.
5. Odpowiedź lub komentarz z poprzedniej sesji/hosta: nie może zmienić panelu bieżącej sesji — zadanie 5.

---

### Task 1: Trwała historia i atomowy zapis decyzji

**Files:** Create `backend/internal/storage/sqlite/migrations/0202_local_workflow_decision.sql`, `backend/internal/storage/sqlite/store/local_workflow_decision_store.go`, `backend/internal/storage/sqlite/store/local_workflow_decision_store_test.go`; modify `backend/internal/domain/local_workflow.go`, `backend/internal/storage/sqlite/queries/local_workflow.sql`; regenerate `backend/internal/storage/sqlite/gen/`.

**Interfaces:**
- Produces `domain.LocalWorkflowDecision` with `ID`, `WorkflowID`, `RequestID`, `Kind`, `Comment`, `Source`, `Cycle`, `ExpectedVersion`, `SnapshotID`, `TestOperationID`, `ReviewRunID`, `CreatedAt`.
- Produces store methods `ListLocalWorkflowDecisions(context.Context, string) ([]domain.LocalWorkflowDecision, error)`, `LocalWorkflowDecisionByRequest(context.Context, string, string) (domain.LocalWorkflowDecision, bool, error)`, `RecordLocalWorkflowDecision(context.Context, int64, domain.LocalWorkflowRun, domain.LocalWorkflowDecision) (bool, error)`.
- `RecordLocalWorkflowDecision` updates the run with expected-version CAS and inserts the decision in one transaction. Neither a conflict nor a failed insert may partially persist either record.

- [x] **Step 1: Write failing tests** `TestWorkflowDecisionAtomicVersionConflict`, `TestWorkflowDecisionRetryIdentity`, `TestWorkflowDecisionSurvivesReopen`: assert zero decisions after failed CAS; one decision for identical retry; changed payload with the same request ID conflicts; history and exact evidence survive reopening the real SQLite store; CDC still comes from the run update.
- [x] **Step 2: Run** `go test -count=1 ./internal/storage/sqlite/store -run TestWorkflowDecision` from `backend`. Expected: FAIL because decision types/methods do not exist.
- [x] **Step 3: Implement** the domain record, additive migration, sqlc queries and store methods above; use existing transaction/write-lock helpers and validate decision ownership and expected version. Do not persist an intent token.
- [x] **Step 4: Generate and verify** `npm run sqlc`, then `go test -race -count=1 ./internal/storage/sqlite/store` and `go test -count=1 ./internal/storage/sqlite`. Expected: PASS including migration round-trip checks and unchanged existing workflow storage tests.

### Task 2: Rozstrzygnięcie w koordynatorze i nowe poprawki Codexa

**Files:** Create `backend/internal/localworkflow/decision.go`, `backend/internal/localworkflow/decision_test.go`; modify `coordinator.go`, `control.go`, `presentation.go`, `prompt.go`, `recovery.go` in the same directory and their relevant tests.

**Interfaces:**
- Consumes Task 1 store methods, existing `Worker`, `Reviews`, workspace admission and snapshot guard.
- Produces `DecisionRequest` with `RequestID`, `IntentID`, `WorkflowID`, `ExpectedVersion`, `Kind`, `Comment`, `SnapshotID`, `TestOperationID`, `ReviewRunID`; `IntentID` is transport-private and never serialized in responses or logs.
- Produces `DecisionChallenge` containing the exact `DecisionRequest` plus `SessionID` and cycle; `DecisionConfirmer.Confirm(context.Context, DecisionChallenge) error`, injected as `Deps.Decisions` and required for new decisions.
- Produces `Coordinator.Decide(context.Context, domain.SessionID, DecisionRequest) (Snapshot, error)`; extends `Snapshot` with `Decisions []domain.LocalWorkflowDecision` and `Accepted bool`, and stage enum with `accepted`.
- Produces `BuildUserFixPrompt(domain.LocalWorkflowRun, domain.LocalWorkflowDecision) string`. Existing reviewer corrections continue to use `BuildFixPrompt`.

- [x] **Step 1: Write failing tests** `TestWorkflowHumanAcceptanceRequiresCurrentEvidence`, `TestWorkflowHumanDecisionRechecksAfterConfirmation`, `TestWorkflowHumanDecisionRetry`, `TestWorkflowUserCorrectionSource`, `TestWorkflowDecisionInvalidationAndRecovery`: reject missing confirmer, failed/missing command result, required findings, wrong cycle/snapshot/evidence, pending work, terminated session, Stop and version change; acceptance sets stage only after confirmation; mutation during confirmation refuses the write; exact retry records once without another confirmation and changed input conflicts.
- [x] **Step 2: Run** `go test -race -count=1 ./internal/localworkflow -run 'TestWorkflow(Human|UserCorrection|Decision)'`. Expected: FAIL because decision orchestration is absent.
- [x] **Step 3: Implement** preflight under run/admission locks, release them before `Confirm`, then reacquire and fully revalidate before the atomic store write. Give confirmation a deadline bounded to 60 seconds. Missing or denied confirmation has stable sentinel errors. Never trust client `Current` or reviewer approval alone. Preserve read-idempotency of already committed requests without applying the old decision to new evidence.
- [x] **Step 4: Implement** accepted-stage derivation and user correction source selection. A user decision is the input identity of its fix operation; both reservation and evaluation use that identity. At the final cycle persist `changes_requested` and `round_limit` without delivery. Resume after an explicit increase uses the same pending user decision. A completed fix advances the cycle and forces fresh verification. Recovery preserves history without recreating observation.
- [x] **Step 5: Extend tests** to return a user comment after an earlier completed correction, replay Evaluate repeatedly, and assert exactly one new delivery containing the comment and original task. At the limit assert no delivery until the limit increases; after file mutation/revert or real store/coordinator reopen assert historical decision remains and `Accepted=false`. Optional suggestions must not become required corrections.
- [x] **Step 6: Verify** `go test -race -count=1 -timeout=5m ./internal/localworkflow ./internal/localsnapshot`. Expected: PASS, including the old loop/Stop/recovery suites. No side effect executes while confirmation is unresolved.

### Task 3: Prywatna intencja i natywny dialog Electrona

**Files:** Create `frontend/src/shared/local-workflow-decisions.ts`, `frontend/src/main/local-workflow-decisions.ts`, `frontend/src/main/local-workflow-decisions.test.ts`, `backend/internal/daemon/local_workflow_decisions.go`, `backend/internal/daemon/local_workflow_decisions_test.go`; modify `frontend/src/main.ts`, `frontend/src/preload.ts`, `backend/internal/daemon/local_workflow_wiring.go`, `backend/internal/daemon/daemon.go`; extend existing broker/link tests only for actual transport behavior being changed.

**Interfaces:**
- Consumes Task 2 `DecisionConfirmer` and the existing authenticated `browserruntime.Broker.Execute` transport. No protocol/listener replacement.
- Shared TS `LocalWorkflowDecisionInput` mirrors the public request excluding `IntentID`; `LocalWorkflowDecisionReply` is `{status: "recorded"}` or `{status: "cancelled"}`. Stable error codes represent conflict/unavailable/invalid input.
- Produces `createLocalWorkflowDecisionHost(options: DecisionHostOptions): LocalWorkflowDecisionHost`, with `request(owner: WebContents, input: LocalWorkflowDecisionInput): Promise<LocalWorkflowDecisionReply>`, `confirm(command: BrowserRuntimeCommand, signal: AbortSignal): Promise<{confirmed: true}>`, `dispose(): void`. `DecisionHostOptions` injects daemon status/request transport, native dialog, time, randomness and owner-window lookup for tests.
- IPC channel `localWorkflow:decide`; preload `aoBridge.localWorkflow.decide(input)`. Main validates sender/frame against trusted shell WebContents before calling `request`.
- Internal broker action `__confirm-local-workflow-decision`; daemon adapter `newLocalWorkflowDecisionConfirmer(broker)` implements `Confirm`. It validates a typed `{confirmed:true}` result, rejecting other shapes and broker errors.

- [x] **Step 1: Write failing Vitest tests** `requiresMatchingPrivateIntent`, `confirmsOnlyExplicitNativeResponse`, `expiresAtSixtySeconds`, `rejectsChangedOwnerAndAbortedConnection`, `deduplicatesRequestsAndRetries`: unknown/mismatched intent never opens a dialog; cancellation/default response never confirms; exactly 60 seconds expires; late success after abort or owner destruction is rejected; different body under the same request ID conflicts; nonce is generated privately and not returned to the renderer; 4000 Unicode characters pass and 4001/NUL fail.
- [x] **Step 2: Run** `npm test -- src/main/local-workflow-decisions.test.ts` from `frontend`. Expected: FAIL because the host module is absent.
- [x] **Step 3: Implement** the isolated host module with 32 cryptographic random bytes, monotonic expiry checks, per-request deduplication and native `dialog.showMessageBox` owned by the initiating window. Default/cancel button is Anuluj. Build the main-to-daemon request using verified local daemon status; never accept a URL/headers from renderer. Match the full request/challenge, not only the nonce. Clean intents when requests end, owner closes or host is disposed.
- [x] **Step 4: Wire** IPC/preload and the internal action in main before the BrowserView dispatch. Register only trusted shell senders and primary frames. Inject the daemon confirmer at coordinator construction; reject a missing confirmer. Do not add the action to the public browser allowlist. Add Go adapter tests for true/false/malformed/broker-error responses and public action rejection in `backend/internal/service/browser/service_test.go`.
- [x] **Step 5: Verify** `npm test -- src/main/local-workflow-decisions.test.ts src/main/browser-runtime-link.test.ts` and `go test -race -count=1 ./internal/daemon ./internal/browserruntime ./internal/service/browser`. Expected: PASS; private action is not publicly dispatchable, existing render-check and browser behavior continue to work.

### Task 4: Kontrakt HTTP i blokada na listenerze LAN

**Files:** Modify `backend/internal/httpd/controllers/local_workflow.go`, `local_workflow_test.go`, `local_workflow_integration_test.go`, `dto.go`, `backend/internal/httpd/apispec/specgen/build.go`, `backend/internal/httpd/lan_listener.go` and its existing tests; regenerate `backend/internal/httpd/apispec/openapi.yaml` and `frontend/src/api/schema.ts`.

**Interfaces:**
- Consumes Task 2 `Decide` on `LocalWorkflowService` and Task 3 main transport.
- Produces `POST /api/v1/sessions/{sessionId}/local-workflow/decision` with `DecideLocalWorkflowRequest` carrying Task 2 request fields and response `LocalWorkflowResponse` extended with decisions/accepted.
- The listener LAN block rejects exactly the session-scoped decision route (including normalized trailing slash), before authentication/router dispatch. A spoofed loopback Host on a LAN socket is still rejected. Other workflow reads/controls retain their existing behavior.

- [x] **Step 1: Write failing HTTP tests** for successful recorded response, strict JSON, extra fields, mismatched evidence, conflict 409, invalid input 422, confirmation denied/unavailable and absent service. Assert no intent token or private execution paths in responses; standard error envelope and request ID survive.
- [x] **Step 2: Write failing LAN tests** with the real LAN wrapper, a forged localhost Host and the exact decision path; assert 404 and zero service/confirmation calls. Assert an unrelated sibling path is not blocked by a broad suffix match.
- [x] **Step 3: Run** `go test -count=1 ./internal/httpd/...`. Expected: new route/response/LAN assertions FAIL before implementation.
- [x] **Step 4: Implement** the transport-only route and typed DTOs; register names and operation in the spec generator. Map confirmation errors distinctly; authorization remains in the service/main boundary. Add exact LAN routing block without changing primary-listener auth/bind or the identity exception.
- [x] **Step 5: Generate and verify** `npm run api` followed by `go test -race -count=1 ./internal/httpd/...`. Expected: PASS with route/spec parity and integration tests; a second `npm run api` leaves generated files unchanged.

### Task 5: Akcje, historia i Accepted w interfejsie

**Files:** Modify `frontend/src/renderer/components/LocalWorkflowPanel.tsx`, `LocalWorkflowHistory.tsx`, `LocalWorkflowBoard.tsx`, their tests, `frontend/src/renderer/hooks/use-local-workflow.ts`, `frontend/src/renderer/lib/local-workflow-api.ts`, `frontend/src/renderer/lib/bridge.ts`; add copy to existing `frontend/src/renderer/i18n/*.json` following locale coverage conventions.

**Interfaces:**
- Consumes the generated Task 4 snapshot and Task 3 preload method. UI writes decisions through preload only; the browser fallback exposes a desktop-required failure and never performs HTTP acceptance.
- Produces hook mutation inputs `{kind: "accept" | "request_changes", version, workflowId, snapshotId, testOperationId, reviewRunId, comment}`. Preserve a request ID across uncertain network retries for identical inputs, reset it for changed inputs. Refresh the exact session/host query after success or conflict.
- Accepted is rendered from the daemon stage/flag; no optimistic acceptance or local decision authority.

- [x] **Step 1: Write failing UI tests** for accept/reject visibility and disabled state, missing desktop bridge/remote host, empty rejection comment, pending duplicate clicks, native cancellation, conflict refresh, current versus historical decision, and column Accepted without reviewer approval auto-acceptance.
- [x] **Step 2: Add owner-change tests**: change session or host with a pending decision response and drafted comment; assert old response/comment cannot alter the new panel. Retry of identical input reuses the request ID, changed comment/evidence gets a new one.
- [x] **Step 3: Run** `npm test -- src/renderer/components/LocalWorkflowPanel.test.tsx src/renderer/components/LocalWorkflowBoard.test.tsx`. Expected: new assertions FAIL before implementation.
- [x] **Step 4: Implement** the buttons/comment field, decision history and hook behavior. Require `ready` plus current evidence and local desktop for actions. Show read-only historical decisions after invalidation/restart. Pending/cancelled/error states do not claim acceptance. Support existing locales and accessible labels.
- [x] **Step 5: Verify** the two component suites, locale coverage tests and `npm run typecheck` from `frontend`; run `npm run frontend:lint` from root. Expected: PASS, with unchanged existing PR-board behavior.

### Task 6: Odbiór, niezależny przegląd i publikacja OB-6

**Files:** Create `docs/ob-6-verification.md`; update this plan's checklist and approved-spec status, the OB-6 Jira record and existing Confluence page. Logs/screenshots go to ignored task-owned paths, not Git. Update only documentation directly needed for OB-6.

**Interfaces:** Consumes the complete implementation and tests from Tasks 1–5. Produces verified commit/push/PR URLs and an honest task status; no automatic merge.

- [x] **Step 1: Run the full selected CI validation** from `docs/ci.md` and actual `.github/workflows` selection for the final diff. Use existing pinned Go 1.27.1 and Node 24.19.0 runtimes rather than shell defaults. Cover backend race suites, build/vet/golangci-lint 2.13.2, frontend full unit/typing/lint, renderer smoke, browser postcondition integration, generation drift, CI contract and dependent shared/mobile/native/CLI suites selected by CI. Redirect verbose logs to this plan's ignored workspace and inspect failures. Expected: every runnable selected job PASS; unavailable native/platform jobs listed explicitly for remote verification.
- [x] **Step 2: Run the actual Electron checkout** following `ao-desktop-dev` in isolated mode under `~/.ao/dev/ob6/`. Exercise positive native acceptance, cancellation, return comment and new verification, limit, file mutation/revert, panel reopen and daemon restart. Confirm main/preload changes were loaded after an Electron restart. Record actual ports/PIDs and data paths; stop only this task's processes after the check. Expected: criteria from the approved spec visibly hold; no changes to user data.
- [x] **Step 3: Obtain one independent whole-change review** following `superpowers:requesting-code-review`, with the approved spec, this plan, final diff, Review Focus and ledger rulings. Resolve important findings with reproducing tests and rerun affected full suites. Expected: no unresolved correctness/security findings. Record any remaining limitation accurately.
- [x] **Step 4: Write the verification report** with commands/results, exact manual scenarios, evidence and gaps. Add screenshots or recordings to the Confluence journal when capture tools support them, without secret material. Test-double scenarios must be clearly separated from OB-7's real-agent acceptance. Expected: a reviewer can trace each OB-6 criterion to evidence.
- [ ] **Step 5: Inspect and publish only OB-6 files.** Recheck `git diff --check`, final diff and generated artifacts, then create a conventional `feat: add human local workflow decisions (OB-6)` commit, push `feature/OB-6`, and open a PR in `grzegorzrzeznikiewicz/orchestrator-board` with base `main`. Follow `.agents/skills/pr-description/SKILL.md` for verified Code changes / Tests / Others counts and read back the published description. Attach the PR through the app tool if available. Expected: one task-specific PR with correct base, scope, header and evidence.
- [ ] **Step 6: Verify remote checks for the final commit.** Fix real failures and rerun affected suites/checks. Update Jira and Confluence with PR/CI, delivered behavior, manual results and exact remaining status. Do not label an unverified task Done or treat PR creation as merge. Expected: required remote CI PASS, unresolved gates explicitly represented, and no premature completion of OB-1.

## Self-review and handoff

Spec coverage: Task 1 covers durable identity/atomicity/CDC; Task 2 acceptance rules, corrections, limits and recovery; Task 3 private intent/native confirmation; Task 4 HTTP/LAN/generation; Task 5 UI/owner boundaries; Task 6 validation/report/publication. Each Review Focus item maps to named tests above. Interfaces match between producer and consumer tasks; `IntentID` is never a public TS input or a response field. Main-to-daemon HTTP and broker callbacks are asynchronous and must not hold coordinator/workspace locks while awaiting the user.

The six tasks are implementation steps within OB-6, not separate Jira deliveries. Their changes remain on this branch and are committed together at the end after verification. No standalone documentation commit/PR precedes the working feature.

Plan review required before implementation. Native execution in this session is preserved from the approved spec; once approved, execute all steps continuously and request input only for an actual blocking decision. Next Jira task starts only after the OB-6 commit, push and PR exist.
