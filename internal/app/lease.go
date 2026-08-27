package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
)

// LeaseRequest acquires or renews a time-limited, mutually exclusive lease on
// one of the six mutex resources.
type LeaseRequest struct {
	ResourceType lineage.ResourceType `json:"resource_type"`
	ResourceID   domain.ResourceID    `json:"resource_id"`
	Holder       domain.TaskID        `json:"holder"`
	Weld         domain.WeldID        `json:"weld,omitempty"`
	Generation   domain.Generation    `json:"generation,omitempty"`
	Start        int64                `json:"start"`
	End          int64                `json:"end"`
}

// LeaseResult is the response to a successful lease acquisition or renewal.
type LeaseResult struct {
	ResourceType lineage.ResourceType `json:"resource_type"`
	ResourceID   domain.ResourceID    `json:"resource_id"`
	Start        int64                `json:"start"`
	End          int64                `json:"end"`
	Acquired     bool                 `json:"acquired"`
}

// AcquireLease acquires a lease, rejecting an overlapping effective interval
// on the same resource, a negative or degenerate interval, or an expired
// (now >= end) lease. The whole operation is atomic.
func (s *Service) AcquireLease(taskID domain.TaskID, opID domain.OperationID, digest string, req LeaseRequest) (*LeaseResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		if req.ResourceType == "" || req.ResourceID == "" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "resource type and id are required")
		}
		if req.End <= req.Start {
			return nil, domain.NewError(domain.CodeDegenerate, "lease end must be after start")
		}
		lease := lineage.Lease{
			ResourceType: req.ResourceType,
			ResourceID:   req.ResourceID,
			Holder:       req.Holder,
			Weld:         req.Weld,
			Generation:   req.Generation,
			Start:        req.Start,
			End:          req.End,
		}
		for _, existing := range st.Leases {
			if existing.Overlaps(lease) {
				return nil, domain.NewError(domain.CodeLeaseConflict,
					fmt.Sprintf("resource %s/%s overlaps an existing lease", req.ResourceType, req.ResourceID))
			}
		}
		st.Leases = append(st.Leases, lease)
		st.appendEvent("LEASE_ACQUIRED", fmt.Sprintf("%s/%s", req.ResourceType, req.ResourceID))
		return jsonResult(LeaseResult{
			ResourceType: req.ResourceType,
			ResourceID:   req.ResourceID,
			Start:        req.Start,
			End:          req.End,
			Acquired:     true,
		})
	})
	if err != nil {
		return nil, err
	}
	var out LeaseResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseLease releases an effective lease before its natural expiry.
func (s *Service) ReleaseLease(taskID domain.TaskID, opID domain.OperationID, digest string, req LeaseRequest) (*LeaseResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		for i := range st.Leases {
			l := &st.Leases[i]
			if l.ResourceType == req.ResourceType && l.ResourceID == req.ResourceID && !l.Released {
				l.Released = true
				l.Reason = "released"
				st.appendEvent("LEASE_RELEASED", fmt.Sprintf("%s/%s", req.ResourceType, req.ResourceID))
				return jsonResult(LeaseResult{
					ResourceType: req.ResourceType,
					ResourceID:   req.ResourceID,
					Acquired:     false,
				})
			}
		}
		return nil, domain.NewError(domain.CodeNotFound, "no active lease for resource")
	})
	if err != nil {
		return nil, err
	}
	var out LeaseResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
