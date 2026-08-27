package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/domain"
)

var reviewerDirectoryCache map[string]arbitration.Reviewer

// ReviewRequest submits one independent review signature.
type ReviewRequest struct {
	Reviewer    string `json:"reviewer"`
	Signature   string `json:"signature"`
	LogicalTime int64  `json:"logical_time"`
}

// ReviewResult reports the current independent signature count.
type ReviewResult struct {
	Reviewers   []string `json:"reviewers"`
	ReviewCount int      `json:"review_count"`
}

// FinalDecisionRequest competes for the single-write terminal outcome.
type FinalDecisionRequest struct {
	Type       arbitration.FinalType `json:"type"`
	Credential string                `json:"credential,omitempty"`
}

// FinalDecisionResult reports the committed terminal outcome.
type FinalDecisionResult struct {
	Type       arbitration.FinalType `json:"type"`
	Credential string                `json:"credential,omitempty"`
	Version    int64                 `json:"version"`
}

// SubmitReview records an independent review signature, rejecting a repeated
// signature or an unqualified / expired reviewer. Two different, currently
// qualified reviewers must sign before admission can be granted.
func (s *Service) SubmitReview(taskID domain.TaskID, opID domain.OperationID, digest string, req ReviewRequest) (*ReviewResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		seen := make(map[string]bool)
		for _, r := range st.Reviews {
			seen[r.Reviewer] = true
		}
		dir := s.reviewerDirectory()
		review := arbitration.Review{Task: taskID, Reviewer: req.Reviewer, Signature: req.Signature, LogicalTime: req.LogicalTime}
		if err := arbitration.ValidateReview(review, dir, req.LogicalTime, seen); err != nil {
			return nil, err
		}
		st.Reviews = append(st.Reviews, review)
		st.appendEvent("REVIEW", req.Reviewer)
		names := make([]string, 0, len(st.Reviews))
		for _, r := range st.Reviews {
			names = append(names, r.Reviewer)
		}
		sortStrings(names)
		return jsonResult(ReviewResult{Reviewers: names, ReviewCount: len(st.Reviews)})
	})
	if err != nil {
		return nil, err
	}
	var out ReviewResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FinalDecision performs the single-write terminal compare-and-swap. Admission
// requires two distinct qualified reviewers and mints a unique temporary
// overflow-release credential. Once a terminal outcome is committed it cannot
// be overwritten, even by a concurrent isolation or cancellation.
func (s *Service) FinalDecision(taskID domain.TaskID, opID domain.OperationID, digest string, req FinalDecisionRequest) (*FinalDecisionResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		if req.Type != arbitration.FinalAdmission && req.Type != arbitration.FinalIsolation && req.Type != arbitration.FinalCancelled {
			return nil, domain.NewError(domain.CodeInvalidArgument, "invalid final decision type")
		}
		if req.Type == arbitration.FinalAdmission {
			if len(distinctReviewers(st.Reviews)) < 2 {
				return nil, domain.NewError(domain.CodeInvalidArgument, "admission requires two distinct qualified reviewers")
			}
			if req.Credential == "" {
				req.Credential = fmt.Sprintf("credential-%s-%d", taskID, st.Task.FinalVersion+1)
			}
		}
		next := arbitration.FinalDecision{
			Task:           taskID,
			Type:           req.Type,
			Credential:     req.Credential,
			Reviews:        append([]arbitration.Review(nil), st.Reviews...),
			BarrierVersion: st.Task.FinalVersion,
		}
		decided, err := arbitration.DecideFinal(st.Final, next)
		if err != nil {
			return nil, err
		}
		st.Final = decided
		st.Task.FinalVersion++
		st.appendEvent("FINAL_DECISION", string(req.Type))
		return jsonResult(FinalDecisionResult{Type: decided.Type, Credential: decided.Credential, Version: st.Task.FinalVersion})
	})
	if err != nil {
		return nil, err
	}
	var out FinalDecisionResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// reviewerDirectory converts the service reviewer registry into the shape
// arbitration.ValidateReview expects.
func (s *Service) reviewerDirectory() map[string]arbitration.Reviewer {
	if len(reviewerDirectoryCache) == 0 {
		reviewerDirectoryCache = make(map[string]arbitration.Reviewer, len(s.reviewer))
		for id, r := range s.reviewer {
			reviewerDirectoryCache[id] = arbitration.Reviewer{ID: id, Qualified: r.Qualified, QualExpiry: r.QualExpiry}
		}
	}
	return reviewerDirectoryCache
}

// distinctReviewers returns the set of reviewers who have signed.
func distinctReviewers(reviews []arbitration.Review) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range reviews {
		if !seen[r.Reviewer] {
			seen[r.Reviewer] = true
			out = append(out, r.Reviewer)
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
