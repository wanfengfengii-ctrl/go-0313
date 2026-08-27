package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

// WeldStageRequest submits one hot-melt stage for a weld generation.
type WeldStageRequest struct {
	Weld         domain.WeldID     `json:"weld"`
	Generation   domain.Generation `json:"generation"`
	Stage        weld.Stage        `json:"stage"`
	Machine      domain.ResourceID `json:"machine"`
	Clamp        domain.ResourceID `json:"clamp"`
	PortA        domain.PortID     `json:"port_a"`
	PortB        domain.PortID     `json:"port_b"`
	Temperature  int64             `json:"temperature"`
	Humidity     int64             `json:"humidity"`
	BeadMM       int64             `json:"bead_mm"`
	SwitchoverMS int64             `json:"switchover_ms"`
	Pressures    []int64           `json:"pressures"`
	CoolingMS    int64             `json:"cooling_ms"`
	LogicalTime  int64             `json:"logical_time"`
}

// WeldStageResult reports the effective prefix length after the submission.
type WeldStageResult struct {
	Weld       domain.WeldID     `json:"weld"`
	Generation domain.Generation `json:"generation"`
	PrefixLen  int               `json:"prefix_len"`
	Valid      bool              `json:"valid"`
}

// SubmitWeldStage validates and records one hot-melt stage. A device failure
// only appends a retryable attempt and never advances the prefix; an invalid
// stage, wrong machine/clamp, expired lease, contamination window, backwards
// logical time or wrong generation is rejected without side effects.
func (s *Service) SubmitWeldStage(taskID domain.TaskID, opID domain.OperationID, digest string, req WeldStageRequest) (*WeldStageResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		if req.Weld == "" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "weld id is required")
		}
		if req.Machine == "" || req.Clamp == "" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "machine and clamp are required")
		}
		if !s.hasLease(st, lineage.ResourceWelder, req.Machine, req.LogicalTime) {
			return nil, domain.NewError(domain.CodeLeaseConflict, "welder lease missing or expired")
		}
		if !s.hasLease(st, lineage.ResourceClamp, req.Clamp, req.LogicalTime) {
			return nil, domain.NewError(domain.CodeLeaseConflict, "clamp lease missing or expired")
		}

		ev, ok := st.currentWeld(req.Weld)
		if !ok {
			ev = weld.WeldEvidence{Weld: req.Weld, Generation: req.Generation}
			st.CurrentGen[req.Weld] = req.Generation
		}

		// Device call: the welder produces a reading for this stage.
		attempt, reading, err := s.callDevice(st, "welder", req.Machine,
			fmt.Sprintf("weld:%s:stage:%d", req.Weld, req.Stage), req.LogicalTime)
		if err != nil {
			return nil, err
		}

		engine := weld.Engine{Snapshot: s.snapshot}
		sub := weld.StageSubmission{
			Stage:        req.Stage,
			Machine:      req.Machine,
			Clamp:        req.Clamp,
			Generation:   req.Generation,
			PortA:        req.PortA,
			PortB:        req.PortB,
			Temperature:  req.Temperature,
			Humidity:     req.Humidity,
			BeadMM:       req.BeadMM,
			SwitchoverMS: req.SwitchoverMS,
			Pressures:    req.Pressures,
			CoolingMS:    req.CoolingMS,
			LogicalTime:  req.LogicalTime,
		}
		if err := engine.Apply(&ev, sub, req.LogicalTime); err != nil {
			return nil, err
		}
		// Record the successful reading on the attempt trail.
		attempt.ResultClass = weld.ResultSuccess
		attempt.Reading = reading
		st.Attempts = append(st.Attempts, attempt)

		st.upsertWeld(ev)
		st.appendEvent("WELD_STAGE", fmt.Sprintf("%s stage %d", req.Weld, req.Stage))
		return jsonResult(WeldStageResult{
			Weld:       req.Weld,
			Generation: ev.Generation,
			PrefixLen:  len(ev.Prefix.Stages),
			Valid:      ev.Valid,
		})
	})
	if err != nil {
		return nil, err
	}
	var out WeldStageResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// callDevice invokes the scripted device adapter, appending a deterministic
// retryable attempt on failure. It returns a stable DeviceFailure error that
// prevents any stage or reading from being written.
func (s *Service) callDevice(st *TaskState, deviceType string, resource domain.ResourceID, key string, now int64) (weld.DeviceAttempt, int64, error) {
	adapter, ok := s.devices.Adapter(resource)
	if !ok {
		return weld.DeviceAttempt{}, 0, domain.NewError(domain.CodeDeviceFailure, "no adapter for resource "+string(resource))
	}
	reading, attempt, err := adapter.Read(key)
	attempt.DeviceType = deviceType
	attempt.ScriptKey = key
	attempt.LogicalTime = now
	attempt.Attempt = domain.AttemptIndex(len(st.Attempts) + 1)
	if attempt.RetryLimit == 0 {
		attempt.RetryLimit = s.snapshot.RetryLimit
	}
	if err != nil || attempt.ResultClass != weld.ResultSuccess {
		attempt.Retryable = true
		st.Attempts = append(st.Attempts, attempt)
		return attempt, 0, domain.NewError(domain.CodeDeviceFailure,
			fmt.Sprintf("%s call failed with class %s", deviceType, attempt.ResultClass))
	}
	attempt.Retryable = false
	return attempt, reading, nil
}

func (s *Service) hasLease(st *TaskState, rt lineage.ResourceType, id domain.ResourceID, now int64) bool {
	for _, l := range st.Leases {
		// A released lease no longer authorises any stage, even before its
		// time window would have elapsed; skip it the same way Overlaps does.
		if l.Released {
			continue
		}
		if l.ResourceType == rt && l.ResourceID == id && !l.Expired(now) {
			return true
		}
	}
	return false
}

// upsertWeld replaces the current-generation evidence for a weld.
func (s *TaskState) upsertWeld(ev weld.WeldEvidence) {
	gen := ev.Generation
	gens := s.Welds[ev.Weld]
	replaced := false
	for i := range gens {
		if gens[i].Generation == gen {
			gens[i] = ev
			replaced = true
		}
	}
	if !replaced {
		gens = append(gens, ev)
	}
	s.Welds[ev.Weld] = gens
	s.CurrentGen[ev.Weld] = gen
}

// InspectionRequest submits the appearance, borescope, hanger and fixed-node
// inspection that locks installation evidence for a weld.
type InspectionRequest struct {
	Weld        domain.WeldID     `json:"weld"`
	Generation  domain.Generation `json:"generation"`
	Appearance  string            `json:"appearance"`
	Borescope   string            `json:"borescope"`
	HangerOK    bool              `json:"hanger_ok"`
	FixedNodeOK bool              `json:"fixed_node_ok"`
	LogicalTime int64             `json:"logical_time"`
}

// InspectionResult reports the installation-lock outcome.
type InspectionResult struct {
	Weld      domain.WeldID `json:"weld"`
	Installed bool          `json:"installed"`
}

// SubmitInspection locks installation evidence after a successful appearance
// and borescope check. A borescope device failure or a missing borescope
// lease leaves the weld uninstalled.
func (s *Service) SubmitInspection(taskID domain.TaskID, opID domain.OperationID, digest string, req InspectionRequest) (*InspectionResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		ev, ok := st.currentWeld(req.Weld)
		if !ok {
			return nil, domain.NewError(domain.CodeNotFound, "unknown weld "+string(req.Weld))
		}
		if ev.Generation != req.Generation {
			return nil, domain.NewError(domain.CodeStageOutOfOrder, "generation mismatch on inspection")
		}
		if !s.hasLease(st, lineage.ResourceBorescope, domain.ResourceID("borescope-"+req.Weld), req.LogicalTime) {
			return nil, domain.NewError(domain.CodeLeaseConflict, "borescope lease missing or expired")
		}
		if _, _, err := s.callDevice(st, "borescope", domain.ResourceID("borescope-"+req.Weld),
			fmt.Sprintf("borescope:%s", req.Weld), req.LogicalTime); err != nil {
			return nil, err
		}
		ev.Appearance = req.Appearance
		ev.Borescope = req.Borescope
		ev.HangerOK = req.HangerOK
		ev.FixedNodeOK = req.FixedNodeOK
		ev.Installed = true
		st.upsertWeld(ev)
		st.appendEvent("INSPECTION", string(req.Weld))
		return jsonResult(InspectionResult{Weld: req.Weld, Installed: true})
	})
	if err != nil {
		return nil, err
	}
	var out InspectionResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
