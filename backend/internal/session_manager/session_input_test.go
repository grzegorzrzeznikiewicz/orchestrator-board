package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func newInputLeaseTestManager() *Manager {
	return &Manager{
		agentOperations:     make(map[domain.SessionID]agentOperationKind),
		switchDecisionInput: make(map[domain.SessionID]domain.AgentSwitchID),
		retainedSwitches:    make(map[domain.SessionID]struct{}),
		inputLeases:         make(map[domain.SessionID]int),
		inputDrained:        make(map[domain.SessionID]chan struct{}),
	}
}

func TestAgentOperationClosesAdmissionThenDrainsExistingInput(t *testing.T) {
	m := newInputLeaseTestManager()
	release, ok := m.AcquireSessionInput("worker-1")
	if !ok {
		t.Fatal("initial input lease was refused")
	}

	beginDone := make(chan error, 1)
	go func() {
		beginDone <- m.beginAgentOperation(context.Background(), "worker-1", agentOperationSwitch)
	}()

	eventuallySessionInput(t, time.Second, func() bool { return m.SessionMutationInProgress("worker-1") })
	if _, admitted := m.AcquireSessionInput("worker-1"); admitted {
		t.Fatal("new input was admitted after mutation intent registered")
	}
	select {
	case err := <-beginDone:
		t.Fatalf("operation began before existing pane write drained: %v", err)
	default:
	}

	release()
	if err := <-beginDone; err != nil {
		t.Fatalf("begin operation after drain: %v", err)
	}
	if _, admitted := m.AcquireSessionInput("worker-1"); admitted {
		t.Fatal("input admitted while operation owns the session")
	}

	m.endAgentOperation("worker-1", agentOperationSwitch)
	releaseAfter, admitted := m.AcquireSessionInput("worker-1")
	if !admitted {
		t.Fatal("input gate did not reopen after operation")
	}
	releaseAfter()
}

func TestAgentSwitchDecisionInputIsNarrowAndDrained(t *testing.T) {
	m := newInputLeaseTestManager()
	id := domain.SessionID("worker-1")
	switchID := domain.AgentSwitchID("switch-1")
	if err := m.beginAgentOperation(context.Background(), id, agentOperationSwitch); err != nil {
		t.Fatal(err)
	}
	m.allowAgentSwitchDecisionInput(id, switchID)

	release, admitted := m.AcquireSessionInput(id)
	if !admitted {
		t.Fatal("human permission input was not admitted")
	}
	closed := make(chan error, 1)
	go func() {
		closed <- m.closeAgentSwitchDecisionInput(context.Background(), id, switchID)
	}()
	select {
	case err := <-closed:
		t.Fatalf("permission lane closed before admitted input drained: %v", err)
	default:
	}
	release()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, admitted := m.AcquireSessionInput(id); admitted {
		t.Fatal("ordinary input remained open after permission lane closed")
	}
}

func TestAgentOperationDrainHonorsContextAndReopensAdmission(t *testing.T) {
	m := newInputLeaseTestManager()
	release, ok := m.AcquireSessionInput("worker-1")
	if !ok {
		t.Fatal("initial input lease was refused")
	}

	ctx, cancel := context.WithCancel(context.Background())
	beginDone := make(chan error, 1)
	go func() {
		beginDone <- m.beginAgentOperation(ctx, "worker-1", agentOperationRestore)
	}()
	eventuallySessionInput(t, time.Second, func() bool { return m.SessionMutationInProgress("worker-1") })
	cancel()
	if err := <-beginDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("begin operation error = %v, want context.Canceled", err)
	}
	if m.SessionMutationInProgress("worker-1") {
		t.Fatal("cancelled operation left input admission closed")
	}
	release()
}

func TestAgentOperationAndInputLeaseAreScopedPerSession(t *testing.T) {
	m := newInputLeaseTestManager()
	if err := m.beginAgentOperation(context.Background(), "worker-1", agentOperationKill); err != nil {
		t.Fatalf("begin worker-1 operation: %v", err)
	}
	defer m.endAgentOperation("worker-1", agentOperationKill)

	if _, ok := m.AcquireSessionInput("worker-1"); ok {
		t.Fatal("worker-1 input admitted during its mutation")
	}
	release, ok := m.AcquireSessionInput("worker-2")
	if !ok {
		t.Fatal("worker-2 input was incorrectly gated by worker-1 mutation")
	}
	release()
	if err := m.beginAgentOperation(context.Background(), "worker-2", agentOperationRestore); err != nil {
		t.Fatalf("worker-2 operation was incorrectly blocked: %v", err)
	}
	m.endAgentOperation("worker-2", agentOperationRestore)
}

func TestResumeWaitsForHibernation(t *testing.T) {
	m := newInputLeaseTestManager()
	id := domain.SessionID("worker-1")
	if err := m.beginAgentOperation(context.Background(), id, agentOperationHibernate); err != nil {
		t.Fatal(err)
	}
	resumed := make(chan error, 1)
	go func() { resumed <- m.beginAgentResume(context.Background(), id) }()
	select {
	case err := <-resumed:
		t.Fatalf("resume overtook hibernation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	m.endAgentOperation(id, agentOperationHibernate)
	if err := <-resumed; err != nil {
		t.Fatal(err)
	}
	m.endAgentResume(id)
}

func TestExplicitOperationsWaitForHibernation(t *testing.T) {
	for _, kind := range []agentOperationKind{agentOperationKill, agentOperationExit, agentOperationSwitch, agentOperationRestore} {
		t.Run(string(kind), func(t *testing.T) {
			m := newInputLeaseTestManager()
			id := domain.SessionID("worker-1")
			if err := m.beginAgentOperation(context.Background(), id, agentOperationHibernate); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			acquired := make(chan error, 1)
			go func() { acquired <- m.beginAgentOperation(ctx, id, kind) }()
			select {
			case err := <-acquired:
				t.Fatalf("explicit operation failed/overtook hibernation: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			m.endAgentOperation(id, agentOperationHibernate)
			if err := <-acquired; err != nil {
				t.Fatal(err)
			}
			m.endAgentOperation(id, kind)
		})
	}
}

func eventuallySessionInput(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

type workflowInputStore struct {
	Store
	state   domain.ActivityState
	blocked bool
	editing bool
}

func (s *workflowInputStore) AcquireLocalReviewWorkspace(context.Context, domain.SessionID) (func(), error) {
	return func() {}, nil
}
func (s *workflowInputStore) BeginLocalReviewEditing(context.Context, domain.SessionID) error {
	s.editing = true
	return nil
}
func (s *workflowInputStore) HasPendingLocalReviewWork(context.Context, domain.SessionID) (bool, error) {
	return false, nil
}
func (s *workflowInputStore) CheckLocalWorkflowWorkerAdmission(context.Context, domain.SessionID) error {
	if s.blocked {
		return domain.ErrLocalWorkflowConflict
	}
	return nil
}
func TestWorkflowVerificationBlocksDirectTerminalInput(t *testing.T) {
	m := newInputLeaseTestManager()
	store := &workflowInputStore{blocked: true}
	m.store = store
	release, ok := m.AcquireSessionInput("worker")
	if ok {
		release()
		t.Fatal("terminal input admitted during verification")
	}
	store.blocked = false
	release, ok = m.AcquireSessionInput("worker")
	if !ok {
		t.Fatal("terminal input not restored")
	}
	release()
	if store.editing {
		t.Fatal("lease acquisition mutated activity before safety checks")
	}
}

func (s *workflowInputStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	state := s.state
	if s.editing {
		state = domain.ActivityActive
	}
	return domain.SessionRecord{ID: "worker", Activity: domain.Activity{State: state}}, true, nil
}

type guardedInputMessenger struct{ sends int }

func (m *guardedInputMessenger) Send(context.Context, domain.SessionID, string) error {
	m.sends++
	return nil
}
func TestWorkflowInputLeasePreservesNudgeSafetyChecks(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityBlocked, domain.ActivityWaitingInput, domain.ActivityExited} {
		t.Run(string(state), func(t *testing.T) {
			ctx := context.Background()
			store := sqlitetest.MustOpen(t)
			if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "input", Path: t.TempDir(), Config: domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}, RegisteredAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			rec, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "input", Kind: domain.KindWorker, Mode: domain.SessionModeTUI, Harness: domain.HarnessCodex, Activity: domain.Activity{State: state}, CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			m := newInputLeaseTestManager()
			m.store = store
			messenger := &guardedInputMessenger{}
			guard := sessionguard.New(store, messenger, nil)
			guard.SetInputLease(m)
			outcome, err := guard.Nudge(ctx, rec.ID, "continue")
			if err != nil {
				t.Fatal(err)
			}
			after, found, err := store.GetSession(ctx, rec.ID)
			if err != nil || !found {
				t.Fatalf("session read: %v", err)
			}
			if outcome == sessionguard.Sent || messenger.sends != 0 || after.Activity.State != state {
				t.Fatalf("unsafe nudge: outcome=%s writes=%d state=%s", outcome, messenger.sends, after.Activity.State)
			}
		})
	}
}

func TestWorkflowGuardMarksEditingOnlyForAcceptedWrite(t *testing.T) {
	m := newInputLeaseTestManager()
	store := &workflowInputStore{state: domain.ActivityIdle}
	m.store = store
	messenger := &guardedInputMessenger{}
	guard := sessionguard.New(store, messenger, nil)
	guard.SetInputLease(m)
	outcome, err := guard.Nudge(context.Background(), "worker", "continue")
	if err != nil || outcome != sessionguard.Sent || messenger.sends != 1 || !store.editing {
		t.Fatalf("accepted write did not invalidate verification: %s, %v, writes=%d editing=%v", outcome, err, messenger.sends, store.editing)
	}
}
