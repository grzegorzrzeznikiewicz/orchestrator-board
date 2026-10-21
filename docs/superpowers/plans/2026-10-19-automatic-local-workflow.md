# Automatic local task workflow
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->

> Implementation of the behavior approved in chat on 2026-10-19. Existing workspace instructions prohibit commits, push and additional worktrees.

**Goal:** A Local task and each new user direction automatically enter execution, verification, independent review and human acceptance.

**Architecture:** Persist task intent atomically with human input, consume it in the daemon only after provisioning and completed execution. The coordinator owns verification planning, retries and evidence. The renderer displays facts, never starts a workflow as a rendering effect.

**Constraints:** Preserve earlier uncommitted OB changes. Never run user tasks as a test. No retrospective arming of old conversations on upgrade. Internal fix/reviewer messages must not create a new intent. Stop remains authoritative until a new explicit user task. Acceptance remains an explicit desktop decision.

## Work

- [ ] Durable, idempotent intent with session/message creation and promotion; real SQLite restart and provenance tests.
- [ ] Daemon consumption, exact user scope, fresh cycles after follow-ups, no duplicate executor prompt; pending intent immediately invalidates readiness.
- [ ] Model-selected verification plan constrained to configured/discovered project checks. Implementation needs real commands; report uses explicit content checks and independent review. Review sees the actual executor output as evidence. No fabricated test successes.
- [ ] Automatic bounded corrections for failed tests and reviewer findings. Safe recovery re-verifies settled operations; uncertain processes are a visible block.
- [ ] UI shows preparation, plan, commands/content checks and blocks, without a required Start action for new tasks.
- [ ] Focused RED/GREEN tests, affected complete suites, types, lint, API/sqlc generation and drift checks, independent review, controlled desktop verification and documentation.

## Review focus

Duplicate messages and daemon restarts must not create duplicate work. Pending/provisioning sessions must not freeze an empty workspace. New instructions invalidate prior approval. A report cannot silently exempt implementation from tests. Recovery must never rerun commands whose old process may still be alive. Missing configuration must be actionable rather than an endless execution card.
