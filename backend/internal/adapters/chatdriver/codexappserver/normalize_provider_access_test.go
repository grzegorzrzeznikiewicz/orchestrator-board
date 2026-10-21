// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package codexappserver

import (
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestCodexProviderAccessClassificationRequiresNativeMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, info   string
		want         domain.ProviderAccessReason
		authSentinel bool
	}{
		{"native unauthorized", `"unauthorized"`, domain.ProviderAccessAuthentication, true},
		{"native quota", `"usageLimitExceeded"`, domain.ProviderAccessRateLimited, false},
		{"connection HTTP 401", `{"httpConnectionFailed":{"httpStatusCode":401}}`, domain.ProviderAccessAuthentication, false},
		{"connection HTTP 429", `{"httpConnectionFailed":{"httpStatusCode":429}}`, domain.ProviderAccessRateLimited, false},
		{"stream HTTP 502", `{"responseStreamDisconnected":{"httpStatusCode":502}}`, domain.ProviderAccessUnavailable, false},
		{"stream HTTP 503", `{"responseStreamDisconnected":{"httpStatusCode":503}}`, domain.ProviderAccessUnavailable, false},
		{"connection HTTP 504", `{"httpConnectionFailed":{"httpStatusCode":504}}`, domain.ProviderAccessUnavailable, false},
		{"forbidden can mean model entitlement", `{"httpConnectionFailed":{"httpStatusCode":403}}`, "", false},
		{"validation HTTP 400", `{"httpConnectionFailed":{"httpStatusCode":400}}`, "", false},
		{"unknown error HTTP 500", `{"httpConnectionFailed":{"httpStatusCode":500}}`, "", false},
		{"missing status", `{"httpConnectionFailed":{"httpStatusCode":null}}`, "", false},
		{"status is not a string", `{"httpConnectionFailed":{"httpStatusCode":"401"}}`, "", false},
		{"unknown native variant", `{"someFutureError":{"httpStatusCode":401}}`, "", false},
		{"ambiguous union", `{"httpConnectionFailed":{"httpStatusCode":401},"responseStreamDisconnected":{"httpStatusCode":503}}`, "", false},
		{"JSON encoded inside prose is not metadata", `"{\"httpConnectionFailed\":{\"httpStatusCode\":401}}"`, "", false},
		{"unstructured HTTP prose", `null`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := normalizeOne(t, "error", `{"threadId":"th1","turnId":"tu1","willRetry":false,"error":{"message":"HTTP 401 unauthorized; 429; 503; timeout","codexErrorInfo":`+tc.info+`}}`)
			var access *domain.ProviderAccessError
			found := errors.As(ev.Err, &access)
			if found != (tc.want != "") || found && (access.Reason != tc.want || access.SafeToRetry) {
				t.Fatalf("classification=%+v found=%v want=%q", access, found, tc.want)
			}
			if errors.Is(ev.Err, ports.ErrChatAuthRequired) != tc.authSentinel {
				t.Fatalf("native authentication sentinel changed: %v", ev.Err)
			}
			if _, safe := domain.ProviderAccessFailure(ev.Err); safe {
				t.Fatal("adapter cannot prove provider termination")
			}
		})
	}
}

func TestCodexTerminalProviderFailureRetainsTypedTransportMetadata(t *testing.T) {
	ev := normalizeOne(t, "turn/completed", `{"threadId":"th1","turn":{"id":"tu1","status":"failed","error":{"message":"transport rejected","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":401}}}}}`)
	var access *domain.ProviderAccessError
	if ev.Kind != ports.ChatEventTurnCompleted || ev.TurnState != domain.TurnStateFailed || !errors.As(ev.Err, &access) || access.Reason != domain.ProviderAccessAuthentication || access.SafeToRetry {
		t.Fatalf("terminal metadata lost: %+v", ev)
	}
}

func TestCodexProviderRetryNotificationDoesNotSettleOrAuthorizeAnotherLaunch(t *testing.T) {
	ev := normalizeOne(t, "error", `{"threadId":"th1","turnId":"tu1","willRetry":true,"error":{"message":"retrying transport","codexErrorInfo":{"responseStreamDisconnected":{"httpStatusCode":503}}}}`)
	if ev.Kind != ports.ChatEventActivityStarted || ev.Err != nil {
		t.Fatalf("native retry was treated as terminal: %+v", ev)
	}
}

func TestCodexAssistantMessageCannotSupplyProviderFailureClassification(t *testing.T) {
	ev := normalizeOne(t, "item/completed", `{"threadId":"th1","turnId":"tu1","item":{"id":"message1","type":"agentMessage","text":"{\"httpConnectionFailed\":{\"httpStatusCode\":401}}"}}`)
	if ev.Kind != ports.ChatEventMessageCompleted || ev.Err != nil {
		t.Fatalf("model output became provider metadata: %+v", ev)
	}
}
