package app

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/domain"
)

func TestModel_ReviewerDirectoryRefresh(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "refresh governs later reviews while retaining audit history",
			run: func(t *testing.T) {
				svc, _ := newTestService(t)
				id := domain.TaskID("directory-refresh")
				lockTask(t, svc, id, validGraph("zone-A", "drain-A", "seg-A", "out-A"), nil)
				svc.SetReviewerDirectory(map[string]ReviewerEntry{
					"reviewer-a": {Qualified: true, QualExpiry: 100},
				})

				first := ReviewRequest{Reviewer: "reviewer-a", Signature: "sig-a", LogicalTime: 10}
				if _, err := svc.SubmitReview(id, "review-a-before-refresh", CanonicalDigest(first), first); err != nil {
					t.Fatalf("review valid before refresh: %v", err)
				}

				svc.SetReviewerDirectory(map[string]ReviewerEntry{
					"reviewer-a": {Qualified: false, QualExpiry: 100},
					"reviewer-b": {Qualified: true, QualExpiry: 20},
					"reviewer-c": {Qualified: true, QualExpiry: 100},
					"reviewer-d": {Qualified: true, QualExpiry: 100},
				})
				rejected := []struct {
					name string
					req  ReviewRequest
				}{
					{name: "revoked", req: ReviewRequest{Reviewer: "reviewer-a", Signature: "sig-a2", LogicalTime: 11}},
					{name: "expired", req: ReviewRequest{Reviewer: "reviewer-b", Signature: "sig-b", LogicalTime: 20}},
					{name: "missing", req: ReviewRequest{Reviewer: "reviewer-x", Signature: "sig-x", LogicalTime: 11}},
				}
				for _, reject := range rejected {
					t.Run(reject.name, func(t *testing.T) {
						_, err := svc.SubmitReview(id, domain.OperationID("reject-"+reject.name), CanonicalDigest(reject.req), reject.req)
						stable, ok := err.(*domain.StableError)
						if !ok || stable.Code != domain.CodeInvalidArgument {
							t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
						}
					})
				}

				for _, reviewer := range []string{"reviewer-c", "reviewer-d"} {
					req := ReviewRequest{Reviewer: reviewer, Signature: "sig-" + reviewer, LogicalTime: 21}
					if _, err := svc.SubmitReview(id, domain.OperationID("accept-"+reviewer), CanonicalDigest(req), req); err != nil {
						t.Fatalf("current qualified reviewer %s rejected: %v", reviewer, err)
					}
				}

				state, ok := svc.GetTask(id)
				if !ok || len(state.Reviews) != 3 || state.Reviews[0].Reviewer != "reviewer-a" {
					t.Fatalf("historical review was not retained: %+v", state)
				}
				final := FinalDecisionRequest{Type: arbitration.FinalAdmission, Credential: "temporary-overflow-release"}
				if _, err := svc.FinalDecision(id, "admit-after-refresh", CanonicalDigest(final), final); err != nil {
					t.Fatalf("admission with current independent reviewers: %v", err)
				}

				svc.SetReviewerDirectory(map[string]ReviewerEntry{})
				overwrite := FinalDecisionRequest{Type: arbitration.FinalIsolation}
				_, err := svc.FinalDecision(id, "overwrite-final", CanonicalDigest(overwrite), overwrite)
				stable, ok := err.(*domain.StableError)
				if !ok || stable.Code != domain.CodeFinalConflict {
					t.Fatalf("expected terminal decision to remain immutable, got %v", err)
				}
			},
		},
		{
			name: "directories are isolated by service instance",
			run: func(t *testing.T) {
				left, _ := newTestService(t)
				right, _ := newTestService(t)
				leftID := domain.TaskID("directory-left")
				rightID := domain.TaskID("directory-right")
				lockTask(t, left, leftID, validGraph("zone-L", "drain-L", "seg-L", "out-L"), nil)
				lockTask(t, right, rightID, validGraph("zone-R", "drain-R", "seg-R", "out-R"), nil)
				left.SetReviewerDirectory(map[string]ReviewerEntry{"left-only": {Qualified: true, QualExpiry: 100}})
				right.SetReviewerDirectory(map[string]ReviewerEntry{"right-only": {Qualified: true, QualExpiry: 100}})

				checks := []struct {
					name     string
					svc      *Service
					task     domain.TaskID
					reviewer string
					wantOK   bool
				}{
					{name: "left accepts own", svc: left, task: leftID, reviewer: "left-only", wantOK: true},
					{name: "left rejects right", svc: left, task: leftID, reviewer: "right-only"},
					{name: "right accepts own", svc: right, task: rightID, reviewer: "right-only", wantOK: true},
					{name: "right rejects left", svc: right, task: rightID, reviewer: "left-only"},
				}
				for i, check := range checks {
					t.Run(check.name, func(t *testing.T) {
						req := ReviewRequest{Reviewer: check.reviewer, Signature: "isolated-signature", LogicalTime: 1}
						_, err := check.svc.SubmitReview(check.task, domain.OperationID(check.name), CanonicalDigest(req), req)
						if check.wantOK && err != nil {
							t.Fatalf("own directory entry rejected: %v", err)
						}
						if !check.wantOK && err == nil {
							t.Fatalf("reviewer from another service accepted (check %d)", i)
						}
					})
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
