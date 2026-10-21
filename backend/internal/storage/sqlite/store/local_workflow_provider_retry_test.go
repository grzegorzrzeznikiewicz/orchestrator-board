// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package store_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestLocalWorkflowProviderRetryPersistsAndProjectsWithoutEvidence(t *testing.T) {
	dataDir := t.TempDir()
	s, err := sqlitetest.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	seedProject(t, s, "retry-cards")
	for _, state := range []string{"waiting", "checking", "recovered", "exhausted", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			worker, err := s.CreateSession(t.Context(), sampleRecord("retry-cards"))
			if err != nil {
				t.Fatal(err)
			}
			retry := map[string]any{"stage": "review", "state": state, "reason": "authentication", "attempt": float64(2), "maxAttempts": float64(3), "failureAt": "2026-10-20T10:00:00Z", "lastAt": "2026-10-20T10:01:00Z", "nextAt": "2026-10-20T10:02:00Z", "operationId": "failed-review", "attemptId": "reserved-retry", "contextId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			raw, err := json.Marshal(map[string]any{"id": "provider-" + state, "sessionId": worker.ID, "phase": "blocked", "cycle": 1, "maxRounds": 3, "providerRetry": retry})
			if err != nil {
				t.Fatal(err)
			}
			var run domain.LocalWorkflowRun
			if err = json.Unmarshal(raw, &run); err != nil {
				t.Fatal(err)
			}
			run.Task = "PRIVATE TASK"
			run.Env = map[string]string{"PRIVATE": "SECRET"}
			run.CreatedAt = histClock
			run.UpdatedAt = histClock
			created, _, err := s.CreateLocalWorkflow(t.Context(), run, "retry-"+state)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = sqlite.OpenPreMigrated(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			saved, found, err := s.GetLocalWorkflow(t.Context(), created.ID)
			if err != nil || !found {
				t.Fatalf("read retry: %v %v", found, err)
			}
			assertRetry := func(label string, value any) {
				t.Helper()
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if err = json.Unmarshal(b, &wire); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(wire["providerRetry"], retry) {
					t.Fatalf("%s lost durable retry facts: %+v", label, wire["providerRetry"])
				}
			}
			assertRetry("full run", saved)
			facts, err := s.ListLocalWorkflowCardFacts(t.Context(), []domain.SessionID{worker.ID})
			if err != nil || len(facts) != 1 || facts[0].Run == nil {
				t.Fatalf("card facts: %+v %v", facts, err)
			}
			assertRetry("compact card", facts[0].Run)
			if facts[0].Run.Task != "" || facts[0].Run.Env != nil {
				t.Fatal("compact retry loaded private evidence")
			}
		})
	}
	t.Run("legacy omitted retry", func(t *testing.T) {
		worker, err := s.CreateSession(t.Context(), sampleRecord("retry-cards"))
		if err != nil {
			t.Fatal(err)
		}
		run, _, err := s.CreateLocalWorkflow(t.Context(), domain.LocalWorkflowRun{ID: "legacy-retry", SessionID: worker.ID, Phase: domain.WorkflowBlocked, Cycle: 1, MaxRounds: 3, CreatedAt: histClock, UpdatedAt: histClock}, "legacy")
		if err != nil {
			t.Fatal(err)
		}
		full, _, err := s.GetLocalWorkflow(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := s.ListLocalWorkflowCardFacts(t.Context(), []domain.SessionID{worker.ID})
		if err != nil || len(facts) != 1 || facts[0].Run == nil {
			t.Fatalf("legacy facts: %+v %v", facts, err)
		}
		if full.ProviderRetry != nil || facts[0].Run.ProviderRetry != nil {
			t.Fatal("legacy workflow gained retry facts")
		}
	})
}
