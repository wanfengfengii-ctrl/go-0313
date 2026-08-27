package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// MaterialOpRequest is one atomic material operation: cut a parent node into
// a child (stub, sample, removed segment or loss) of an exact integer
// millimetre length, optionally binding the child to a port.
type MaterialOpRequest struct {
	Parent      domain.MaterialID   `json:"parent"`
	Child       domain.MaterialID   `json:"child"`
	Kind        domain.MaterialKind `json:"kind"`
	Length      domain.LengthMM     `json:"length_mm"`
	BindPort    domain.PortID       `json:"bind_port,omitempty"`
	Disposition domain.Disposition  `json:"disposition,omitempty"`
}

// MaterialOpResult is the response to a successful material operation.
type MaterialOpResult struct {
	TaskID    domain.TaskID     `json:"task_id"`
	Child     domain.MaterialID `json:"child"`
	Length    domain.LengthMM   `json:"length_mm"`
	Remaining domain.LengthMM   `json:"remaining_mm"`
}

// MaterialOp cuts a parent material and optionally binds the resulting child
// to a port, atomically. Any identity, port, inventory or conservation breach
// rolls back the whole transaction.
func (s *Service) MaterialOp(taskID domain.TaskID, opID domain.OperationID, digest string, req MaterialOpRequest) (*MaterialOpResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		// Validate every identity, port and conservation breach before mutating
		// state, so a failed operation leaves the in-memory task untouched. The
		// order matters: a cut that fails the port check must not have already
		// carved the child into the lineage, or the partial side effect would
		// leak into subsequent queries until a restart reloaded the snapshot.
		if req.Child == "" || req.Parent == "" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "parent and child are required")
		}
		if req.Kind == "" {
			req.Kind = domain.KindStub
		}
		if req.Length <= 0 {
			return nil, domain.NewError(domain.CodeDegenerate, "cut length must be positive")
		}
		if req.BindPort != "" {
			if !st.portExists(req.BindPort) {
				return nil, domain.NewError(domain.CodeNotFound, "unknown port "+string(req.BindPort))
			}
			if prev, ok := st.PortBindings[req.BindPort]; ok {
				return nil, domain.NewError(domain.CodePortInUse,
					fmt.Sprintf("port %s already bound to material %s", req.BindPort, prev))
			}
		}
		if err := st.Lineage.Convert(req.Parent, req.Child, req.Kind, req.Length); err != nil {
			return nil, err
		}
		disp := req.Disposition
		if disp == "" {
			disp = dispositionForKind(req.Kind, req.BindPort != "")
		}
		if req.BindPort != "" {
			st.PortBindings[req.BindPort] = req.Child
		}
		st.Lineage.Dispositions[req.Child] = disp
		st.appendEvent("MATERIAL_OP", fmt.Sprintf("%s -> %s (%dmm)", req.Parent, req.Child, req.Length))
		return jsonResult(MaterialOpResult{
			TaskID:    taskID,
			Child:     req.Child,
			Length:    req.Length,
			Remaining: st.Lineage.Remaining(req.Parent),
		})
	})
	if err != nil {
		return nil, err
	}
	var out MaterialOpResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *TaskState) portExists(id domain.PortID) bool {
	for _, p := range s.Task.Graph.Ports {
		if p.ID == id {
			return true
		}
	}
	return false
}

// dispositionForKind maps a material kind to its default disposition. A bound
// port implies an installed segment; samples and removed segments carry their
// own dispositions; otherwise the node stays available.
func dispositionForKind(kind domain.MaterialKind, bound bool) domain.Disposition {
	if bound {
		return domain.DispositionInstalled
	}
	switch kind {
	case domain.KindSample:
		return domain.DispositionSample
	case domain.KindRemoved:
		return domain.DispositionRemoved
	case domain.KindLoss:
		return domain.DispositionLoss
	default:
		return domain.DispositionAvailable
	}
}
