// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestConversationTurnOutputIsScopedAndExcludesRolledBackMessages(t *testing.T) {
	s, session, conversation := seededChatConversation(t)
	seedBranchTurns(t, s, session, conversation)
	reader, ok := any(s).(interface {
		ConversationTurnOutput(context.Context, string, string) ([]domain.ConversationMessage, error)
	})
	if !ok {
		t.Fatal("workflow output has no turn-scoped query and must load the whole transcript")
	}
	ctx := t.Context()
	if err := s.BindTurnToProvider(ctx, "turn-2", "provider-turn-2", testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAssistantDelta(ctx, conversation.ID, "streaming-item", "provider-turn-2", "unfinished", "stream-delta", testNow); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		conversation, turn string
		want               []string
	}{
		{conversation.ID, "turn-1", []string{"first answer"}},
		{conversation.ID, "turn-2", []string{}},
		{"another-conversation", "turn-1", []string{}},
		{conversation.ID, "missing", []string{}},
	} {
		got, err := reader.ConversationTurnOutput(ctx, tc.conversation, tc.turn)
		if err != nil || !reflect.DeepEqual(texts(got), tc.want) {
			t.Fatalf("output %s/%s: %v %v, want %v", tc.conversation, tc.turn, texts(got), err, tc.want)
		}
	}
	if _, err := s.RollbackTurns(ctx, conversation.ID, "turn-1", testNow); err != nil {
		t.Fatal(err)
	}
	got, err := reader.ConversationTurnOutput(ctx, conversation.ID, "turn-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("rolled-back output leaked: %+v %v", got, err)
	}
}

func TestConversationTurnOutputPreservesBranchCutoff(t *testing.T) {
	s, session, conversation := seededChatConversation(t)
	seedBranchTurns(t, s, session, conversation)
	reader, ok := any(s).(interface {
		ConversationTurnOutput(context.Context, string, string) ([]domain.ConversationMessage, error)
	})
	if !ok {
		t.Fatal("turn-scoped workflow output query missing")
	}
	child := domain.ConversationBranch{ID: "output-child", ConversationID: conversation.ID, SessionID: session.ID,
		ProviderConversationID: "thread-output-child", ParentBranchID: conversation.ActiveBranchID,
		ForkAfterTurnID: "turn-1", ReplacedTurnID: "turn-2", ForkAfterSequence: 1}
	if err := s.CreateConversationBranch(t.Context(), child, testNow); err != nil {
		t.Fatal(err)
	}
	activateTestBranch(t, s, session, conversation, child.ID, child.ProviderConversationID, "generation-child")
	got, err := reader.ConversationTurnOutput(t.Context(), conversation.ID, "turn-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("answer after branch cutoff leaked: %+v %v", got, err)
	}
}
