// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package chat_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestHumanInputQueuesUntilWorkflowVerificationReleasesWorkspace(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	project, ok, err := h.st.GetProject(ctx, string(testProject))
	if err != nil || !ok {
		t.Fatal(err)
	}
	session, _, err := h.st.GetSession(ctx, testSession)
	if err != nil {
		t.Fatal(err)
	}
	session.Kind = domain.KindWorker
	if err = h.st.UpdateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	project.Config.ReviewMode = domain.ReviewTargetLocal
	if err = h.st.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	run, _, err := h.st.CreateLocalWorkflow(ctx, domain.LocalWorkflowRun{ID: uuid.NewString(), SessionID: testSession, Phase: domain.WorkflowTesting, Cycle: 1, MaxRounds: 3, CreatedAt: time.Now(), UpdatedAt: time.Now()}, "verification-fence")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "Implement the next correction", Origin: domain.MessageOriginHuman, ClientMessageID: "human-during-tests"})
	if err != nil || turn.State != domain.TurnStateQueued {
		t.Fatalf("human input must be preserved in queue: %+v %v", turn, err)
	}
	if err = h.svc.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	durable, err := h.st.TurnByID(ctx, turn.ID)
	if err != nil || durable.State != domain.TurnStateQueued {
		t.Fatalf("verification fence bypassed: %+v %v", durable, err)
	}
	run.Phase = domain.WorkflowStopped
	now := time.Now()
	run.ClosedAt = &now
	if _, err = h.st.AdvanceLocalWorkflow(ctx, run.Version, run, nil); err != nil {
		t.Fatal(err)
	}
	if err = h.svc.DrainQueued(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	durable, err = h.st.TurnByID(ctx, turn.ID)
	if err != nil || durable.State == domain.TurnStateQueued {
		t.Fatalf("human work did not resume: %+v %v", durable, err)
	}
}

type workflowQueueBoundary struct {
	*sqlite.Store
	armed    atomic.Bool
	selected chan struct{}
	proceed  chan struct{}
}

func (s *workflowQueueBoundary) NextQueuedTurn(ctx context.Context, id string) (domain.QueuedTurn, error) {
	queued, err := s.Store.NextQueuedTurn(ctx, id)
	if err == nil && s.armed.CompareAndSwap(true, false) {
		close(s.selected)
		<-s.proceed
	}
	return queued, err
}

func TestWorkflowCancellationSerializesWithSelectedQueuedTurn(t *testing.T) {
	provider := newFakeConversation()
	provider.onSend = func(id string) { provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id}) }
	var boundary *workflowQueueBoundary
	h := newHarnessWithConversationAndStore(t, interruptingReviewConversation{provider}, func(st *sqlite.Store) chatsvc.Store {
		boundary = &workflowQueueBoundary{Store: st, selected: make(chan struct{}), proceed: make(chan struct{})}
		return boundary
	})
	ctx := context.Background()
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "initial"}); err != nil {
		t.Fatal(err)
	}
	turn, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "required correction", ClientMessageID: "workflow-fix"})
	if err != nil {
		t.Fatal(err)
	}
	boundary.armed.Store(true)
	var release sync.Once
	defer release.Do(func() { close(boundary.proceed) })
	provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: "provider-turn-1", TurnState: domain.TurnStateCompleted})
	select {
	case <-boundary.selected:
	case <-time.After(3 * time.Second):
		t.Fatal("queue was not selected for dispatch")
	}
	worker := localworkflow.NewSessionWorker(h.st, nil, h.svc)
	done := make(chan error, 1)
	go func() { done <- worker.Interrupt(ctx, testSession, "workflow-fix") }()
	select {
	case err := <-done:
		t.Fatalf("cancellation returned before queue selection was serialized: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release.Do(func() { close(boundary.proceed) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := h.st.TurnByID(ctx, turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("correction survived cancellation: %+v", current)
		}
		time.Sleep(time.Millisecond)
	}
}

type workflowAdmissionBoundary struct {
	*sqlite.Store
	armed   atomic.Bool
	entered chan struct{}
}

func (s *workflowAdmissionBoundary) AcquireLocalReviewWorkspace(ctx context.Context, id domain.SessionID) (func(), error) {
	if s.armed.CompareAndSwap(true, false) {
		close(s.entered)
	}
	return s.Store.AcquireLocalReviewWorkspace(ctx, id)
}

type workflowStopBoundary struct {
	localworkflow.Worker
	store   *workflowAdmissionBoundary
	service *chatsvc.Service
	once    sync.Once
	sent    chan error
}

func (w *workflowStopBoundary) Observe(ctx context.Context, id domain.SessionID, op string) (localworkflow.WorkerObservation, error) {
	run, found, err := w.store.CurrentLocalWorkflow(ctx, id)
	if err != nil {
		return localworkflow.WorkerObservation{}, err
	}
	if found && run.StopRequested && op == "" {
		w.once.Do(func() {
			w.store.armed.Store(true)
			go func() {
				sendCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				_, err := w.service.Send(sendCtx, id, ports.ChatUserMessage{Text: "racing message"})
				w.sent <- err
			}()
			<-w.store.entered
		})
	}
	return w.Worker.Observe(ctx, id, op)
}
func TestWorkflowStopDoesNotInvertChatSendAdmissionOrder(t *testing.T) {
	provider := newFakeConversation()
	provider.onSend = func(id string) { provider.emit(ports.ChatEvent{Kind: ports.ChatEventTurnStarted, ProviderTurnID: id}) }
	var boundary *workflowAdmissionBoundary
	h := newHarnessWithConversationAndStore(t, interruptingReviewConversation{provider}, func(st *sqlite.Store) chatsvc.Store {
		boundary = &workflowAdmissionBoundary{Store: st, entered: make(chan struct{})}
		return boundary
	})
	ctx := context.Background()
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "initial"}); err != nil {
		t.Fatal(err)
	}
	worker := &workflowStopBoundary{Worker: localworkflow.NewSessionWorker(h.st, nil, h.svc), store: boundary, service: h.svc, sent: make(chan error, 1)}
	coordinator := localworkflow.New(localworkflow.Deps{Store: h.st, Worker: worker})
	defer func() { _ = coordinator.Close(ctx) }()
	started, err := coordinator.Start(ctx, testSession, "stop-lock-integration")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.Stop(ctx, testSession, started.Run.Version); err != nil {
		t.Fatal(err)
	}
	if sendErr := <-worker.sent; errors.Is(sendErr, context.DeadlineExceeded) {
		t.Fatalf("Stop held admission while waiting for Chat sendMu: %v", sendErr)
	}
}
