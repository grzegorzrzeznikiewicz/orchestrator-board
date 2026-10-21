//go:build !darwin

// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localsnapshot

// Other filesystems retain conservative notification handling. Nonzero ctime
// alone does not prove enough precision to detect a permission change/revert.
func canFilterReadChmod(string) bool { return false }
