package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/topology"
)

// CreateTaskRequest reserves a draft task id.
type CreateTaskRequest struct {
	TaskID domain.TaskID `json:"task_id"`
}

// CreateTaskResult is the response to task creation.
type CreateTaskResult struct {
	TaskID  domain.TaskID `json:"task_id"`
	Version int64         `json:"version"`
}

// LockTaskRequest carries the full construction summary that is validated and
// locked in a single transaction: the summary version, the integer-coordinate
// hydraulic graph, and the root material batches.
type LockTaskRequest struct {
	TaskID         domain.TaskID           `json:"task_id"`
	SummaryVersion int64                   `json:"summary_version"`
	Graph          topology.HydraulicGraph `json:"graph"`
	Materials      []MaterialSpec          `json:"materials"`
}

// LockTaskResult is the response to a successful lock.
type LockTaskResult struct {
	TaskID   domain.TaskID `json:"task_id"`
	Version  int64         `json:"version"`
	Locked   bool          `json:"locked"`
	EventSeq int64         `json:"event_seq"`
}

// CreateTask reserves a draft task. It is idempotent: creating the same id
// twice with the same content replays the original result.
func (s *Service) CreateTask(opID domain.OperationID, digest string, req CreateTaskRequest) (*CreateTaskResult, error) {
	if req.TaskID == "" {
		return nil, domain.NewError(domain.CodeInvalidArgument, "task_id is required")
	}
	body, err := s.runCommand(req.TaskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if st.Task.LockState == topology.LockStateLocked {
			return nil, domain.NewError(domain.CodeInvalidArgument, "task already locked")
		}
		st.appendEvent("TASK_CREATED", string(req.TaskID))
		return jsonResult(CreateTaskResult{TaskID: req.TaskID, Version: st.Task.EventSeq})
	})
	if err != nil {
		return nil, err
	}
	var out CreateTaskResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LockTask validates the summary version, integer geometry, topology,
// diameter transitions and materials, then locks the task against an
// immutable rule snapshot in one atomic transaction.
func (s *Service) LockTask(opID domain.OperationID, digest string, req LockTaskRequest) (*LockTaskResult, error) {
	if req.TaskID == "" {
		return nil, domain.NewError(domain.CodeInvalidArgument, "task_id is required")
	}
	body, err := s.runCommand(req.TaskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if st.Task.LockState == topology.LockStateLocked {
			return nil, domain.NewError(domain.CodeInvalidArgument, "task already locked")
		}
		if int64(s.snapshot.Version) != req.SummaryVersion {
			return nil, domain.NewError(domain.CodeStaleSummary,
				fmt.Sprintf("summary version %d does not match catalog version %d", req.SummaryVersion, s.snapshot.Version))
		}
		if err := s.snapshot.ValidateTopology(&req.Graph); err != nil {
			return nil, err
		}
		if err := s.loadMaterials(st, req.Materials); err != nil {
			return nil, err
		}
		st.Task.Graph = req.Graph
		st.Task.SnapshotID = fmt.Sprintf("snapshot-%d", s.snapshot.Version)
		st.Task.LockState = topology.LockStateLocked
		st.appendEvent("TASK_LOCKED", string(req.TaskID))
		return jsonResult(LockTaskResult{
			TaskID:   req.TaskID,
			Version:  st.Task.EventSeq,
			Locked:   true,
			EventSeq: st.Task.EventSeq,
		})
	})
	if err != nil {
		return nil, err
	}
	var out LockTaskResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// loadMaterials registers root material nodes, rejecting duplicate ids and
// negative lengths before the task is locked.
func (s *Service) loadMaterials(st *TaskState, specs []MaterialSpec) error {
	for _, m := range specs {
		if m.ID == "" {
			return domain.NewError(domain.CodeInvalidArgument, "material id is required")
		}
		if m.Length < 0 {
			return domain.NewError(domain.CodeDegenerate, "negative length for material "+string(m.ID))
		}
		if err := st.Lineage.AddNode(lineage.MaterialIdentity{
			ID:     m.ID,
			Batch:  m.Batch,
			Kind:   m.Kind,
			Length: m.Length,
		}); err != nil {
			return err
		}
	}
	return nil
}
