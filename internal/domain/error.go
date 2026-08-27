package domain

import (
	"fmt"
	"strings"
)

// ErrorCode is a stable, machine-readable code returned by every command
// boundary. Callers compare against these constants rather than parsing
// human-readable text.
type ErrorCode string

const (
	CodeOK                  ErrorCode = "OK"
	CodeInvalidArgument     ErrorCode = "INVALID_ARGUMENT"
	CodeNotFound            ErrorCode = "NOT_FOUND"
	CodeInternal            ErrorCode = "INTERNAL"
	CodeStaleSummary        ErrorCode = "STALE_SUMMARY"
	CodeDuplicatePort       ErrorCode = "DUPLICATE_PORT"
	CodeCycle               ErrorCode = "CYCLE"
	CodeDisconnected        ErrorCode = "DISCONNECTED"
	CodeIllegalDiameter     ErrorCode = "ILLEGAL_DIAMETER_TRANSITION"
	CodeDegenerate          ErrorCode = "DEGENERATE_DIMENSION"
	CodeOverflow            ErrorCode = "FIXED_POINT_OVERFLOW"
	CodeDivideByZero        ErrorCode = "DIVIDE_BY_ZERO"
	CodeLineageBreach       ErrorCode = "LINEAGE_NOT_CONSERVED"
	CodePortInUse           ErrorCode = "PORT_IN_USE"
	CodeLeaseConflict       ErrorCode = "LEASE_CONFLICT"
	CodeStageOutOfOrder     ErrorCode = "STAGE_OUT_OF_ORDER"
	CodeDeviceFailure       ErrorCode = "DEVICE_FAILURE"
	CodeIdempotencyConflict ErrorCode = "IDEMPOTENCY_CONFLICT"
	CodeFinalConflict       ErrorCode = "FINAL_DECISION_CONFLICT"
)

// StableError is the domain error protocol. Reasons are kept sorted and
// deduplicated so two identical violations always serialise identically,
// satisfying the "排序确定的拒绝原因" (deterministically ordered reasons)
// requirement. Version records the transaction version at the point of
// failure so retries can compare against the original.
type StableError struct {
	Code    ErrorCode
	Reasons []string
	Version int64
}

func (e *StableError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if len(e.Reasons) == 0 {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, strings.Join(e.Reasons, "; "))
}

// NewError builds a StableError from a code and zero or more reasons. Reasons
// are normalised to a sorted, unique order.
func NewError(code ErrorCode, reasons ...string) *StableError {
	seen := make(map[string]struct{}, len(reasons))
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sortStrings(out)
	return &StableError{Code: code, Reasons: out}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
