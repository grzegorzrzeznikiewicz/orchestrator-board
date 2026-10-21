// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package domain

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// LocalWorkflowRequestID bounds an internal source identity without exposing
// arbitrary provider/client identifiers as operation paths or request keys.
func LocalWorkflowRequestID(source, identity string) string {
	return fmt.Sprintf("%s:%x", source, sha256.Sum256([]byte(identity)))
}

// LocalWorkflowIntent durably identifies newly requested local work. It is
// private intake data, never a derived session status or a public task prompt.
type LocalWorkflowIntent struct {
	SessionID  SessionID
	RequestID  string
	Prompt     string
	TurnID     string
	State      string
	WorkflowID string
	CreatedAt  time.Time
	ConsumedAt *time.Time
}
