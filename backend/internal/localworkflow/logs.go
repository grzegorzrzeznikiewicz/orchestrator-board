// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TestLog contains a bounded read of one test output stream.
type TestLog struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// TestLog reads an owned test log beneath the configured data root.
func (c *Coordinator) TestLog(ctx context.Context, id domain.SessionID, testID, stream string) (TestLog, error) {
	if stream != "stdout" && stream != "stderr" {
		return TestLog{}, fmt.Errorf("%w: stream must be stdout or stderr", domain.ErrLocalWorkflowInvalid)
	}
	snap, e := c.Get(ctx, id)
	if e != nil {
		return TestLog{}, e
	}
	var selected *domain.LocalWorkflowTestResult
	for i := range snap.Tests {
		if snap.Tests[i].ID == testID {
			selected = &snap.Tests[i]
			break
		}
	}
	if selected == nil {
		return TestLog{}, domain.ErrLocalWorkflowNotFound
	}
	test := *selected
	path := test.StdoutLog
	truncated := test.StdoutTruncated
	if stream == "stderr" {
		path = test.StderrLog
		truncated = test.StderrTruncated
	}
	rel, e := filepath.Rel(c.deps.DataDir, path)
	if e != nil {
		return TestLog{}, e
	}
	root, e := os.OpenRoot(c.deps.DataDir)
	if e != nil {
		return TestLog{}, e
	}
	defer func() { _ = root.Close() }()
	file, e := root.Open(rel)
	if e != nil {
		return TestLog{}, e
	}
	defer func() { _ = file.Close() }()
	const limit = 256 * 1024
	b, e := io.ReadAll(io.LimitReader(file, limit+1))
	if e != nil {
		return TestLog{}, e
	}
	if len(b) > limit {
		b = b[:limit]
		truncated = true
	}
	return TestLog{Text: string(b), Truncated: truncated}, nil
}
