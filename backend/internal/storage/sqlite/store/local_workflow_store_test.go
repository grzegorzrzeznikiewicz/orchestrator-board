package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestLocalWorkflowPersistenceCASAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlitetest.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	if err = s.UpsertProject(ctx, domain.ProjectRecord{ID: "wf", Path: "/wf", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	worker, err := s.CreateSession(ctx, sampleRecord("wf"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	run := domain.LocalWorkflowRun{ID: "flow", SessionID: worker.ID, Version: 1, Phase: domain.WorkflowWaiting, Cycle: 1, MaxRounds: 3, Task: "fix it", TestCommands: []string{"printf ok"}, Env: map[string]string{"TEST_ENV": "secret"}, WorkspacePath: "/wf", CreatedAt: now, UpdatedAt: now}
	got, created, err := s.CreateLocalWorkflow(ctx, run, "request-1")
	if err != nil || !created || got.ID != "flow" {
		t.Fatalf("create: %+v %v %v", got, created, err)
	}
	if _, created, err = s.CreateLocalWorkflow(ctx, run, "request-1"); err != nil || created {
		t.Fatalf("retry: %v %v", created, err)
	}
	other := run
	other.ID = "other"
	if _, _, err = s.CreateLocalWorkflow(ctx, other, "request-2"); err == nil {
		t.Fatal("parallel workflow allowed")
	}
	other.Task = "different payload"
	if _, _, err = s.CreateLocalWorkflow(ctx, other, "request-1"); err == nil {
		t.Fatal("request id silently changed payload")
	}
	op := domain.LocalWorkflowOperation{ID: "test-op", WorkflowID: run.ID, Kind: "tests", Cycle: 1, Attempt: 1, State: "reserved", Generation: "gen", ClientMessageID: "delivery", StartedAt: now}
	next := run
	next.Phase = domain.WorkflowTesting
	var wg sync.WaitGroup
	outcomes := make(chan bool, 2)
	for range 2 {
		wg.Go(func() {
			ok, e := s.AdvanceLocalWorkflow(ctx, 1, next, &op)
			if e != nil {
				t.Error(e)
			}
			outcomes <- ok
		})
	}
	wg.Wait()
	close(outcomes)
	count := 0
	for ok := range outcomes {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CAS winners = %d", count)
	}
	next.Version = 2
	next.Phase = domain.WorkflowReviewing
	op.State = "succeeded"
	op.CompletedAt = &now
	code := 0
	test := domain.LocalWorkflowTestResult{ID: "test-result", OperationID: op.ID, CommandIndex: 0, Command: "printf ok", Status: "succeeded", ExitCode: &code, StdoutLog: "/private/log", StartedAt: now, CompletedAt: &now}
	if ok, e := s.CompleteLocalWorkflowOperation(ctx, 2, op, next, []domain.LocalWorkflowTestResult{test}); e != nil || !ok {
		t.Fatalf("complete: %v %v", ok, e)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = nil
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, ok, err := s.CurrentLocalWorkflow(ctx, worker.ID)
	if err != nil || !ok || saved.Version != 3 || saved.Phase != domain.WorkflowReviewing || saved.Task != "fix it" || saved.Env["TEST_ENV"] != "secret" || saved.WorkspacePath != "/wf" {
		t.Fatalf("reopen: %+v %v %v", saved, ok, err)
	}
	ops, err := s.ListLocalWorkflowOperations(ctx, run.ID)
	if err != nil || len(ops) != 1 || ops[0].Generation != "gen" || ops[0].State != "succeeded" || ops[0].ClientMessageID != "delivery" {
		t.Fatalf("operations: %+v %v", ops, err)
	}
	tests, err := s.ListLocalWorkflowTests(ctx, run.ID)
	if err != nil || len(tests) != 1 || tests[0].StdoutLog != "/private/log" || tests[0].ExitCode == nil || *tests[0].ExitCode != 0 {
		t.Fatalf("tests: %+v %v", tests, err)
	}
}

func TestLocalWorkflowBlocksWorkerDuringVerification(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "wf", Path: "/wf", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateSession(ctx, sampleRecord("wf"))
	if err != nil {
		t.Fatal(err)
	}
	r := domain.LocalWorkflowRun{ID: "exclusive", SessionID: w.ID, Phase: domain.WorkflowTesting, Cycle: 1, MaxRounds: 3, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if _, _, err = s.CreateLocalWorkflow(ctx, r, "req"); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginLocalReviewEditing(ctx, w.ID); err == nil {
		t.Fatal("worker dispatch allowed during tests")
	}
	r.Phase = domain.WorkflowFixing
	if ok, e := s.AdvanceLocalWorkflow(ctx, 1, r, nil); e != nil || !ok {
		t.Fatalf("advance %v %v", ok, e)
	}
	if err = s.BeginLocalReviewEditing(ctx, w.ID); err != nil {
		t.Fatalf("correction dispatch blocked: %v", err)
	}
}

func TestLocalWorkflowFixCannotReachReplacementController(t *testing.T) {
	for _, tc := range []struct {
		name, generation string
		want             bool
	}{{"same controller", "chat:generation-root", true}, {"replacement", "chat:old-controller", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s, worker, conversation := seededChatConversation(t)
			ctx := context.Background()
			now := time.Now()
			run := domain.LocalWorkflowRun{ID: "flow", SessionID: worker.ID, Phase: domain.WorkflowFixing, Cycle: 1, MaxRounds: 3, CreatedAt: now, UpdatedAt: now}
			if _, _, e := s.CreateLocalWorkflow(ctx, run, "start"); e != nil {
				t.Fatal(e)
			}
			op := domain.LocalWorkflowOperation{ID: "fix", WorkflowID: run.ID, Kind: "fix", Cycle: 1, Attempt: 1, State: "reserved", Generation: tc.generation, ClientMessageID: "fix-delivery", StartedAt: now}
			if ok, e := s.AdvanceLocalWorkflow(ctx, 1, run, &op); e != nil || !ok {
				t.Fatalf("reserve %v %v", ok, e)
			}
			created, e := s.AppendUserMessage(ctx, conversation.ID, worker.ID, "generation-root", domain.ConversationMessage{ID: "fix-message", ClientMessageID: "fix-delivery", Text: "Fix required issue", Origin: domain.MessageOriginAutomation}, "fix-turn", now)
			if created != tc.want || (e == nil) != tc.want {
				t.Fatalf("wrong generation admission: created=%v err=%v", created, e)
			}
		})
	}
}
