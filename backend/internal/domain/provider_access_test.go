// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestProviderAccessRetryRequiresKnownReasonAndSettledExecution(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want ProviderAccessReason
		ok   bool
	}{
		{"settled auth", &ProviderAccessError{Reason: ProviderAccessAuthentication, SafeToRetry: true}, ProviderAccessAuthentication, true},
		{"settled quota", &ProviderAccessError{Reason: ProviderAccessRateLimited, SafeToRetry: true}, ProviderAccessRateLimited, true},
		{"settled service", &ProviderAccessError{Reason: ProviderAccessUnavailable, SafeToRetry: true}, ProviderAccessUnavailable, true},
		{"unsettled", &ProviderAccessError{Reason: ProviderAccessAuthentication}, "", false},
		{"unknown reason", &ProviderAccessError{Reason: "new_reason", SafeToRetry: true}, "", false},
		{"opaque provider prose", errors.New("HTTP 401 unauthorized"), "", false},
		{"joined uncertainty overrides flag", errors.Join(&ProviderAccessError{Reason: ProviderAccessAuthentication, SafeToRetry: true}, ErrProviderExecutionUncertain), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ProviderAccessFailure(tc.err)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("retry=%q/%v want=%q/%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestProviderAccessErrorPreservesCauseWithoutDisplayingProviderSecrets(t *testing.T) {
	cause := errors.New("provider included PRIVATE-CREDENTIAL in its error")
	err := &ProviderAccessError{Reason: ProviderAccessAuthentication, Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("typed failure lost its cause")
	}
	if strings.Contains(err.Error(), "PRIVATE-CREDENTIAL") {
		t.Fatal("typed recovery message exposed provider details")
	}
}
