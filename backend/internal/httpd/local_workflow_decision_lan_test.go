// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package httpd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/mobilebridge"
)

func TestLANBlocksDesktopWorkflowDecisionBeforeAuth(t *testing.T) {
	calls := 0
	m := NewMobileLAN(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }), "host", 0, nil, nil)
	m.SetPasswordHash(mobilebridge.HashPassword("valid-password"))
	for _, suffix := range []string{"decision", "decision/"} {
		for _, bearer := range []string{"", "Bearer valid-password"} {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/worker/local-workflow/"+suffix, nil)
			req.Host = "127.0.0.1"
			req.Header.Set("Authorization", bearer)
			res := httptest.NewRecorder()
			m.handler.ServeHTTP(res, req)
			if res.Code != 404 || calls != 0 {
				t.Fatalf("desktop decision exposed to LAN: %d calls=%d", res.Code, calls)
			}
		}
	}
	for _, suffix := range []string{"decision-notes", "start", ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/worker/local-workflow/"+suffix, nil)
		req.Header.Set("Authorization", "Bearer valid-password")
		res := httptest.NewRecorder()
		m.handler.ServeHTTP(res, req)
		if res.Code != 204 {
			t.Fatalf("unrelated workflow path blocked: %s %d", suffix, res.Code)
		}
	}
}
