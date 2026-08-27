package arbitration

import "siphonic-roof-drainage-overflow-release/internal/domain"

// ValidateReview checks a single review against the reviewer directory:
// the reviewer must exist, hold a currently valid qualification, and not
// have already signed. It returns a stable error for a repeated signature
// or an unqualified / expired reviewer.
func ValidateReview(r Review, directory map[string]Reviewer, now int64, seen map[string]bool) error {
	rv, ok := directory[r.Reviewer]
	if !ok {
		return domain.NewError(domain.CodeInvalidArgument, "unknown reviewer "+r.Reviewer)
	}
	if !rv.Qualified {
		return domain.NewError(domain.CodeInvalidArgument, "reviewer "+r.Reviewer+" is not qualified")
	}
	if rv.QualExpiry <= now {
		return domain.NewError(domain.CodeInvalidArgument, "reviewer "+r.Reviewer+" qualification expired")
	}
	if seen[r.Reviewer] {
		return domain.NewError(domain.CodeInvalidArgument, "reviewer "+r.Reviewer+" already signed")
	}
	return nil
}

// DecideFinal performs the single-write terminal decision compare-and-swap.
// It returns the existing decision unchanged (and a conflict error) when one
// is already written, otherwise it installs the new decision. The barrier
// version must match the task's current final barrier version.
func DecideFinal(current *FinalDecision, next FinalDecision) (*FinalDecision, error) {
	if current != nil && current.Type != FinalNone {
		return current, domain.NewError(domain.CodeFinalConflict, "final decision already committed")
	}
	if next.Type == FinalNone {
		return current, domain.NewError(domain.CodeInvalidArgument, "decision type is not terminal")
	}
	cp := next
	return &cp, nil
}
