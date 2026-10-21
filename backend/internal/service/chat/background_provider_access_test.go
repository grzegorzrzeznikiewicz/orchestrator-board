// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package chat_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

type providerAccessConversation struct {
	*fakeConversation
	failure        error
	cleanupFailure error
	terminated     bool
	terminalEvent  bool
}

func (c *providerAccessConversation) StartDeferredTurn(id string) error {
	if c.terminalEvent {
		c.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: id, TurnState: domain.TurnStateFailed, Err: c.failure})
		return nil
	}
	c.emit(ports.ChatEvent{Kind: ports.ChatEventError, ProviderTurnID: id, Err: c.failure})
	return nil
}

func TestBackgroundProviderAccessRetainsFailureFromNativeTerminalTurn(t *testing.T) {
	failure := &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication}
	conv := &providerAccessConversation{fakeConversation: newFakeConversation(), failure: failure, terminalEvent: true}
	_, err := providerAccessService(fakeDriver{conv: conv}).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
	if reason, safe := domain.ProviderAccessFailure(err); reason != domain.ProviderAccessAuthentication || !safe {
		t.Fatalf("native terminal failure lost: %q/%v err=%v", reason, safe, err)
	}
}

func (*providerAccessConversation) DiscardDeferredTurn(string) {}

func (c *providerAccessConversation) Terminate() error {
	c.terminated = true
	if c.cleanupFailure != nil {
		return c.cleanupFailure
	}
	return c.Close()
}

func providerAccessService(driver fakeDriver) *chatsvc.Service {
	return chatsvc.New(chatsvc.Options{Drivers: fakeRegistry{driver: driver}, NewID: func() string { return "provider-access-test" }})
}

func TestBackgroundProviderAccessRetryNeedsSuccessfulTermination(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		reason  domain.ProviderAccessReason
	}{
		{"native authentication", ports.ErrChatAuthRequired, domain.ProviderAccessAuthentication},
		{"native HTTP authentication", &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication}, domain.ProviderAccessAuthentication},
		{"native rate limit", &domain.ProviderAccessError{Reason: domain.ProviderAccessRateLimited}, domain.ProviderAccessRateLimited},
		{"native unavailable", &domain.ProviderAccessError{Reason: domain.ProviderAccessUnavailable}, domain.ProviderAccessUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conv := &providerAccessConversation{fakeConversation: newFakeConversation(), failure: tc.failure}
			_, err := providerAccessService(fakeDriver{conv: conv}).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
			got, safe := domain.ProviderAccessFailure(err)
			if got != tc.reason || !safe || !conv.terminated || !errors.Is(err, tc.failure) {
				t.Fatalf("reason=%q safe=%v terminated=%v err=%v", got, safe, conv.terminated, err)
			}
		})
	}
}

func TestBackgroundProviderAccessCleanupFailurePreservesBothErrorsAndDeniesRetry(t *testing.T) {
	providerErr := &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, SafeToRetry: true}
	cleanupErr := errors.New("cannot confirm provider shutdown")
	conv := &providerAccessConversation{fakeConversation: newFakeConversation(), failure: providerErr, cleanupFailure: cleanupErr}
	_, err := providerAccessService(fakeDriver{conv: conv}).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
	if !errors.Is(err, cleanupErr) || !errors.Is(err, providerErr) || !errors.Is(err, domain.ErrProviderExecutionUncertain) {
		t.Fatalf("lost cleanup/provider uncertainty: %v", err)
	}
	if _, safe := domain.ProviderAccessFailure(err); safe {
		t.Fatal("failed provider teardown enabled another provider launch")
	}
	// The fixture's failed Terminate intentionally did not close its event stream.
	_ = conv.Close()
}

func TestBackgroundProviderAccessStartFailureIsNeverAssumedSettled(t *testing.T) {
	failure := &domain.ProviderAccessError{Reason: domain.ProviderAccessUnavailable, SafeToRetry: true}
	driver := fakeDriver{start: func(ports.ChatStartConfig) (ports.ChatConversation, error) { return nil, failure }}
	_, err := providerAccessService(driver).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
	if _, safe := domain.ProviderAccessFailure(err); safe {
		t.Fatal("Start failure supplied no teardown evidence but enabled retry")
	}
	if !errors.Is(err, failure) {
		t.Fatal("Start failure cause was lost")
	}
}

func TestBackgroundProviderAccessCannotPromoteControllerCloseToTermination(t *testing.T) {
	failure := &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, SafeToRetry: true}
	conv := &deferredConversation{fakeConversation: newFakeConversation()}
	conv.start = func(id string) error {
		conv.emit(ports.ChatEvent{Kind: ports.ChatEventError, ProviderTurnID: id, Err: failure})
		return nil
	}
	_, err := providerAccessService(fakeDriver{conv: conv}).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
	if _, safe := domain.ProviderAccessFailure(err); safe {
		t.Fatal("controller Close is not proof detached provider work ended")
	}
}

func TestBackgroundProviderAccessNeverClassifiesOpaqueProseOrCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("HTTP 401 Missing bearer; retry after timeout 429"), context.DeadlineExceeded, context.Canceled} {
		conv := &providerAccessConversation{fakeConversation: newFakeConversation(), failure: failure}
		_, err := providerAccessService(fakeDriver{conv: conv}).RunBackgroundTask(t.Context(), domain.HarnessCodex, ports.ChatStartConfig{}, "Return a plan")
		if _, safe := domain.ProviderAccessFailure(err); safe {
			t.Fatalf("opaque/cancelled failure classified as retryable: %v", err)
		}
	}
}
