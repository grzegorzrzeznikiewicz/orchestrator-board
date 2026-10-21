// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package domain

import "errors"

// ProviderAccessReason is an allowlisted provider observation, never inferred
// from provider prose, model output, or a failed workflow validation.
type ProviderAccessReason string

// Provider access observations distinguish authentication, capacity, and availability.
const (
	ProviderAccessAuthentication ProviderAccessReason = "authentication"
	ProviderAccessRateLimited    ProviderAccessReason = "rate_limited"
	ProviderAccessUnavailable    ProviderAccessReason = "unavailable"
)

// Valid reports whether the reason belongs to the recovery protocol.
func (r ProviderAccessReason) Valid() bool {
	return r == ProviderAccessAuthentication || r == ProviderAccessRateLimited || r == ProviderAccessUnavailable
}

// ErrProviderExecutionUncertain fences retries when provider work may survive
// its controller. Availability does not establish that earlier work has ended.
var ErrProviderExecutionUncertain = errors.New("provider execution completion is uncertain")

// ProviderAccessError carries native failure classification. Adapters leave
// SafeToRetry false; only a boundary that confirms provider teardown may set it.
// Cause stays private and is preserved for errors.Is/As without exposing raw
// provider details through the safe recovery message.
type ProviderAccessError struct {
	Reason      ProviderAccessReason
	SafeToRetry bool
	Cause       error `json:"-"`
}

func (e *ProviderAccessError) Error() string {
	switch e.Reason {
	case ProviderAccessAuthentication:
		return "Provider authentication is unavailable."
	case ProviderAccessRateLimited:
		return "Provider capacity is temporarily limited."
	default:
		return "Provider access is unavailable."
	}
}

func (e *ProviderAccessError) Unwrap() error { return e.Cause }

// ProviderAccessFailure returns only a classified failure whose prior work has
// a confirmed end. Joined uncertainty always wins over a nested positive flag.
func ProviderAccessFailure(err error) (ProviderAccessReason, bool) {
	if errors.Is(err, ErrProviderExecutionUncertain) {
		return "", false
	}
	var failure *ProviderAccessError
	if !errors.As(err, &failure) || failure == nil || !failure.Reason.Valid() || !failure.SafeToRetry {
		return "", false
	}
	return failure.Reason, true
}
