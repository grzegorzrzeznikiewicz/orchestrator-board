// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type newerHumanPromptDuringProjection struct {
	*fakeStore
	newerAt  time.Time
	conflict bool
}

func (s *newerHumanPromptDuringProjection) UpdateSessionFromActivitySignal(ctx context.Context, rec domain.SessionRecord, revision int64) (bool, error) {
	if !s.conflict {
		s.conflict = true
		current := s.sessions[rec.ID]
		current.Revision++
		current.Metadata.LatestUserPrompt = "newer accepted human task"
		current.Metadata.LatestUserPromptAt = s.newerAt
		current.Metadata.ConversationCheckpointState = domain.ConversationCheckpointPrompt
		current.Metadata.ConversationCheckpointGeneration = "launch-current"
		current.Metadata.ConversationCheckpointNativeID = "native-current"
		current.Metadata.ConversationCheckpointTurnID = "newer-turn"
		s.sessions[rec.ID] = current
		return false, nil
	}
	return s.fakeStore.UpdateSessionFromActivitySignal(ctx, rec, revision)
}

func TestLocalWorkflowIntentLosingProjectionCannotArmAnOlderPromptOnRetry(t *testing.T) {
	m, st, _ := newManager()
	rec := working("mer-1")
	rec.Kind = domain.KindWorker
	rec.Harness = domain.HarnessCodex
	rec.Mode = domain.SessionModeTUI
	rec.Metadata.RuntimeLaunchID = "launch-current"
	rec.Metadata.AgentSessionID = "native-current"
	rec.Metadata.AgentSessionIDLaunchID = "launch-current"
	st.sessions[rec.ID] = rec
	olderAt := time.Now().UTC()
	m.store = &newerHumanPromptDuringProjection{fakeStore: st, newerAt: olderAt.Add(time.Second)}
	if err := m.ApplyActivitySignal(ctx, rec.ID, ports.ActivitySignal{Valid: true, State: domain.ActivityActive, Event: "user-prompt-submit", LaunchID: "launch-current", AgentSessionID: "native-current", ProviderTurnID: "older-turn", LatestUserPrompt: "older delayed human task", ConversationCheckpointOrigin: domain.ConversationCheckpointOriginHuman, Timestamp: olderAt}); err != nil {
		t.Fatal(err)
	}
	current := st.sessions[rec.ID]
	if current.Metadata.LatestUserPrompt != "newer accepted human task" || current.LocalWorkflowIntent != nil {
		t.Fatalf("stale retry armed an older task: %+v", current.LocalWorkflowIntent)
	}
}

func TestLocalWorkflowIntentRequiresNewExplicitOwnedHumanTerminalTurn(t *testing.T) {
	for _, name := range []string{"human", "coordination", "unknown-origin", "stale-launch", "subagent", "no-turn-identity", "chat"} {
		t.Run(name, func(t *testing.T) {
			m, st, _ := newManager()
			rec := working("mer-1")
			rec.Kind = domain.KindWorker
			rec.Harness = domain.HarnessCodex
			rec.Mode = domain.SessionModeTUI
			rec.Metadata.RuntimeLaunchID = "launch-current"
			rec.Metadata.AgentSessionID = "native-current"
			rec.Metadata.AgentSessionIDLaunchID = "launch-current"
			st.sessions[rec.ID] = rec
			signal := ports.ActivitySignal{Valid: true, State: domain.ActivityActive, Event: "user-prompt-submit", LaunchID: "launch-current", AgentSessionID: "native-current", ProviderTurnID: "turn-1", LatestUserPrompt: "fix rounding and run tests", ConversationCheckpointOrigin: domain.ConversationCheckpointOriginHuman, Timestamp: time.Now().UTC()}
			switch name {
			case "coordination":
				signal.ConversationCheckpointOrigin = domain.ConversationCheckpointOriginCoordination
				signal.CoordinationID = "workflow-fix"
			case "unknown-origin":
				signal.ConversationCheckpointOrigin = domain.ConversationCheckpointOriginUnknown
			case "stale-launch":
				signal.LaunchID = "launch-old"
			case "subagent":
				signal.SubagentID = "subagent-1"
			case "no-turn-identity":
				signal.ProviderTurnID = ""
			case "chat":
				rec.Mode = domain.SessionModeChat
				rec.Metadata.ControllerGeneration = "chat-current"
				st.sessions[rec.ID] = rec
				signal.ControllerGeneration = "chat-current"
			}
			if err := m.ApplyActivitySignal(ctx, rec.ID, signal); err != nil {
				t.Fatal(err)
			}
			intent := st.sessions[rec.ID].LocalWorkflowIntent
			if name == "human" {
				if intent == nil || intent.Prompt != "fix rounding and run tests" || intent.TurnID != "turn-1" || intent.RequestID == "" {
					t.Fatalf("accepted human turn lacks intent: %+v", intent)
				}
			} else if intent != nil {
				t.Fatalf("non-human or stale turn armed workflow: %+v", intent)
			}
		})
	}
}

func TestLocalWorkflowIntentClaudeQueuedPromptsUseDistinctSubmissionIdentity(t *testing.T) {
	m, st, _ := newManager()
	rec := working("mer-1")
	rec.Kind = domain.KindWorker
	rec.Harness = domain.HarnessClaudeCode
	rec.Mode = domain.SessionModeTUI
	rec.Metadata.RuntimeLaunchID = "launch-current"
	rec.Metadata.AgentSessionID = "native-current"
	rec.Metadata.AgentSessionIDLaunchID = "launch-current"
	st.sessions[rec.ID] = rec
	var firstRequest string
	for i, submission := range []string{"submission-1", "submission-2"} {
		if err := m.ApplyActivitySignal(ctx, rec.ID, ports.ActivitySignal{Valid: true, State: domain.ActivityActive, Event: "user-prompt-submit", LaunchID: "launch-current", AgentSessionID: "native-current", ProviderTurnID: "shared-active-prompt", SubmissionID: submission, LatestUserPrompt: "additional user direction", ConversationCheckpointOrigin: domain.ConversationCheckpointOriginHuman, Timestamp: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
		intent := st.sessions[rec.ID].LocalWorkflowIntent
		if intent == nil || intent.TurnID != submission {
			t.Fatalf("missing distinct submitted prompt: %+v", intent)
		}
		if i == 0 {
			firstRequest = intent.RequestID
		} else if firstRequest == intent.RequestID {
			t.Fatal("two separately submitted human prompts share one workflow request")
		}
	}
}
