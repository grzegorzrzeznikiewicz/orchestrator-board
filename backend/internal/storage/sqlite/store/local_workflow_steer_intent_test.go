// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestLocalWorkflowSteerIntentPreservesHumanScopeAndDoesNotRearmOnReplay(t *testing.T) {
	ctx := context.Background()
	s, worker, conversation := localWorkflowSteerFixture(t)
	at := histClock.Add(time.Minute)
	activity := workflowSteerActivity(t, "human-guidance", "human", "", "  Check rounding only.\nKeep existing exports unchanged.  ")
	if err := s.UpsertActivity(ctx, conversation, "provider-worker-turn", activity, at); err != nil {
		t.Fatal(err)
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("accepted human steer has no single durable intent: %+v %v", pending, err)
	}
	intent := pending[0]
	if intent.SessionID != worker || intent.Prompt != "  Check rounding only.\nKeep existing exports unchanged.  " || intent.TurnID != "worker-turn" || !intent.CreatedAt.Equal(at) {
		t.Fatalf("steer scope or accepted turn changed: %+v", intent)
	}
	if _, err := workflowIntentQueue(t, s).ConsumeLocalWorkflowIntent(ctx, worker, intent.RequestID, "workflow-for-guidance", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertActivity(ctx, conversation, "provider-worker-turn", activity, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	activity.ID = "replayed-provider-activity"
	activity.ProviderItemID = "steer:replayed-provider-item"
	if err := s.UpsertActivity(ctx, conversation, "provider-worker-turn", activity, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	pending, err = workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("replayed accepted steer rearmed workflow: %+v %v", pending, err)
	}
	workerRecord, _, err := s.GetSession(ctx, worker)
	if err != nil {
		t.Fatal(err)
	}
	if workerRecord.Metadata.LatestUserPrompt != "" || !workerRecord.Metadata.LatestUserPromptAt.IsZero() {
		t.Fatal("steer overwrote the main user-message checkpoint")
	}
}

func TestLocalWorkflowSteerIntentRequiresHumanWorkerConversation(t *testing.T) {
	for _, name := range []string{"automation", "automation-with-sender", "unknown-origin", "blank", "orchestrator", "pr", "reviewer", "missing-turn"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, worker, conversation := localWorkflowSteerFixture(t)
			origin, sender, prompt, providerTurn := "human", "", "verify the new scope", "provider-worker-turn"
			switch name {
			case "automation", "automation-with-sender":
				origin = "automation"
				if name == "automation-with-sender" {
					record := sampleRecord("hist")
					record.Kind = domain.KindOrchestrator
					created, err := s.CreateSession(ctx, record)
					if err != nil {
						t.Fatal(err)
					}
					sender = string(created.ID)
				}
			case "unknown-origin":
				origin = ""
			case "blank":
				prompt = " \n "
			case "orchestrator":
				record, _, err := s.GetSession(ctx, worker)
				if err != nil {
					t.Fatal(err)
				}
				record.Kind = domain.KindOrchestrator
				if err := s.UpdateSession(ctx, record); err != nil {
					t.Fatal(err)
				}
			case "pr":
				if _, err := s.UpdateProjectSettings(ctx, "hist", "", domain.ProjectConfig{ReviewMode: domain.ReviewTargetPR}); err != nil {
					t.Fatal(err)
				}
			case "reviewer":
				review := domain.Review{ID: "steer-review", SessionID: worker, ProjectID: "hist", Harness: domain.ReviewerCodex, CreatedAt: histClock, UpdatedAt: histClock}
				if err := s.UpsertReview(ctx, review); err != nil {
					t.Fatal(err)
				}
				created, err := s.CreateReviewConversation(ctx, "steer-review-conversation", review.ID, "hist", worker, histClock)
				if err != nil {
					t.Fatal(err)
				}
				conversation = created.ID
				if claimed, err := s.ClaimReviewChatController(ctx, review.ID, "review-provider", "review-generation", histClock); err != nil || !claimed {
					t.Fatalf("claim reviewer: %v %v", claimed, err)
				}
				if created, err := s.AppendReviewUserMessage(ctx, conversation, worker, review.ID, "review-generation", domain.ConversationMessage{ID: "review-opening-message", Origin: domain.MessageOriginAutomation, Text: "review current changes"}, "review-turn", histClock); err != nil || !created {
					t.Fatalf("append reviewer turn: %v %v", created, err)
				}
				providerTurn = "review-provider-turn"
				if err := s.BindTurnToProvider(ctx, "review-turn", providerTurn, histClock); err != nil {
					t.Fatal(err)
				}
			case "missing-turn":
				providerTurn = "unknown-provider-turn"
			}
			activity := workflowSteerActivity(t, "excluded-guidance", origin, sender, prompt)
			if err := s.UpsertActivity(ctx, conversation, providerTurn, activity, histClock.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
			if err != nil || len(pending) != 0 {
				t.Fatalf("nonhuman or nonworker steer armed workflow: %+v %v", pending, err)
			}
		})
	}
}

func TestLocalWorkflowSteerIntentCommitsOnlyWithAcceptedDelivery(t *testing.T) {
	ctx := context.Background()
	s, _, conversation := localWorkflowSteerFixture(t)
	at := histClock.Add(time.Minute)
	activity := workflowSteerActivity(t, "accepted-delivery", "human", "", "check only the requested category filter")
	if err := s.CompleteSteerDelivery(ctx, conversation, "delivery-accepted", "provider-worker-turn", activity, at); err == nil {
		t.Fatal("completion without a reserved delivery succeeded")
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("failed delivery transaction left a workflow intent: %+v %v", pending, err)
	}
	if _, created, err := s.ReserveSteerDelivery(ctx, conversation, "delivery-accepted", `{"text":"check only the requested category filter","origin":"human"}`, at); err != nil || !created {
		t.Fatalf("reserve: %v %v", created, err)
	}
	if err := s.CompleteSteerDelivery(ctx, conversation, "delivery-accepted", "provider-worker-turn", activity, at); err != nil {
		t.Fatal(err)
	}
	pending, err = workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 1 || pending[0].Prompt != "check only the requested category filter" || pending[0].TurnID != "worker-turn" {
		t.Fatalf("accepted delivery did not commit its exact workflow intent: %+v %v", pending, err)
	}
}

func TestLocalWorkflowSteerIntentWithoutClientIDKeepsDistinctAcceptedGuidance(t *testing.T) {
	ctx := context.Background()
	s, _, conversation := localWorkflowSteerFixture(t)
	for i, prompt := range []string{"verify category rounding", "also cover an empty budget"} {
		activity := workflowSteerActivity(t, []string{"first-guidance", "second-guidance"}[i], "human", "", prompt)
		detail, err := json.Marshal(map[string]string{"event": "steer", "origin": "human", "text": prompt})
		if err != nil {
			t.Fatal(err)
		}
		activity.Detail = detail
		if err := s.UpsertActivity(ctx, conversation, "provider-worker-turn", activity, histClock.Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := workflowIntentQueue(t, s).ListPendingLocalWorkflowIntents(ctx)
	if err != nil || len(pending) != 2 || pending[0].RequestID == pending[1].RequestID || pending[0].Prompt != "verify category rounding" || pending[1].Prompt != "also cover an empty budget" {
		t.Fatalf("legacy steers lost their individual accepted scopes: %+v %v", pending, err)
	}
}

func localWorkflowSteerFixture(t *testing.T) (*sqlite.Store, domain.SessionID, string) {
	t.Helper()
	ctx := context.Background()
	s, worker, conversation := conversationFixture(t)
	if _, err := s.UpdateProjectSettings(ctx, "hist", "", domain.ProjectConfig{ReviewMode: domain.ReviewTargetLocal}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendUserMessage(ctx, conversation, worker, "gen-1", domain.ConversationMessage{ID: "worker-opening-message", Origin: domain.MessageOriginAutomation, Text: "existing task"}, "worker-turn", histClock); err != nil {
		t.Fatal(err)
	}
	if err := s.BindTurnToProvider(ctx, "worker-turn", "provider-worker-turn", histClock); err != nil {
		t.Fatal(err)
	}
	return s, worker, conversation
}

func workflowSteerActivity(t *testing.T, id, origin, sender, prompt string) domain.ConversationActivity {
	t.Helper()
	detail, err := json.Marshal(map[string]string{"event": "steer", "origin": origin, "senderSessionId": sender, "text": prompt, "clientMessageId": id})
	if err != nil {
		t.Fatal(err)
	}
	return domain.ConversationActivity{ID: "activity-" + id, ProviderItemID: "steer:" + id, Kind: domain.ActivityKindSystem, Status: domain.ActivityStatusCompleted, Detail: detail}
}
