package app

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
)

// WaterTestRequest advances one phase of a zone water test with a scripted
// gauge or flow-meter reading.
type WaterTestRequest struct {
	Zone            domain.ZoneID              `json:"zone"`
	Phase           arbitration.WaterTestPhase `json:"phase"`
	Value           int64                      `json:"value"`
	LogicalTime     int64                      `json:"logical_time"`
	DrainDurationMS int64                      `json:"drain_duration_ms,omitempty"`
}

// WaterTestResult reports the session phase and barrier state.
type WaterTestResult struct {
	Zone        domain.ZoneID              `json:"zone"`
	Phase       arbitration.WaterTestPhase `json:"phase"`
	VolumeMM3   int64                      `json:"volume_mm3"`
	DrainedOK   bool                       `json:"drained_ok"`
	BarrierOpen bool                       `json:"barrier_open"`
}

// StartWaterTest computes the zone pipe volume with fixed-point arithmetic
// and opens the fill phase. It requires a water-zone lease.
func (s *Service) StartWaterTest(taskID domain.TaskID, opID domain.OperationID, digest string, zone domain.ZoneID, now int64) (*WaterTestResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		if !s.hasLease(st, lineage.ResourceWaterZone, domain.ResourceID("water-"+zone), now) {
			return nil, domain.NewError(domain.CodeLeaseConflict, "water-zone lease missing or expired")
		}
		vol, err := s.zoneVolume(st, zone)
		if err != nil {
			return nil, err
		}
		// Each task/zone gets its own heap-allocated session. Sharing a
		// single session variable across tasks crosstalks water-test state:
		// a second task's StartWaterTest would overwrite the first task's
		// record, and advancing one task would mutate the other's phase and
		// readings through the shared pointer.
		sess := &arbitration.WaterTestSession{
			Task:      taskID,
			Zone:      zone,
			Phase:     arbitration.WaterPhaseFill,
			VolumeMM3: vol,
			FillTime:  now,
		}
		st.WaterTests[zone] = sess
		st.appendEvent("WATER_TEST_START", string(zone))
		return jsonResult(WaterTestResult{Zone: zone, Phase: sess.Phase, VolumeMM3: vol})
	})
	if err != nil {
		return nil, err
	}
	var out WaterTestResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AdvanceWaterTest moves a session through fill, hold, drain and empty using
// scripted gauge/flow readings and fixed-point drain-flow computation. A
// gauge or flow-meter failure keeps the barrier closed.
func (s *Service) AdvanceWaterTest(taskID domain.TaskID, opID domain.OperationID, digest string, req WaterTestRequest) (*WaterTestResult, error) {
	body, err := s.runCommand(taskID, opID, digest, func(st *TaskState) ([]byte, error) {
		if err := requireLocked(st); err != nil {
			return nil, err
		}
		sess, ok := st.WaterTests[req.Zone]
		if !ok {
			return nil, domain.NewError(domain.CodeNotFound, "no water test for zone "+string(req.Zone))
		}
		switch req.Phase {
		case arbitration.WaterPhaseFill:
			if sess.Phase != arbitration.WaterPhaseFill {
				return nil, domain.NewError(domain.CodeStageOutOfOrder, "fill out of phase")
			}
			// Water-level gauge device call.
			if _, _, err := s.callDevice(st, "water-level", domain.ResourceID("gauge-"+req.Zone),
				fmt.Sprintf("gauge:%s:fill", req.Zone), req.LogicalTime); err != nil {
				return nil, err
			}
			if req.Value < s.snapshot.WaterTest.MinWaterLevel {
				return nil, domain.NewError(domain.CodeInvalidArgument, "fill level below minimum")
			}
			sess.Readings = append(sess.Readings, arbitration.WaterReading{Kind: "WATER_LEVEL", Value: req.Value, LogicalTime: req.LogicalTime, Valid: true})
			sess.Phase = arbitration.WaterPhaseHold
		case arbitration.WaterPhaseHold:
			if sess.Phase != arbitration.WaterPhaseHold {
				return nil, domain.NewError(domain.CodeStageOutOfOrder, "hold out of phase")
			}
			if req.LogicalTime < sess.FillTime+s.snapshot.WaterTest.HoldMS {
				return nil, domain.NewError(domain.CodeStageOutOfOrder, "hold time not yet elapsed")
			}
			sess.Phase = arbitration.WaterPhaseDrain
		case arbitration.WaterPhaseDrain:
			if sess.Phase != arbitration.WaterPhaseDrain {
				return nil, domain.NewError(domain.CodeStageOutOfOrder, "drain out of phase")
			}
			if _, _, err := s.callDevice(st, "flow", domain.ResourceID("flow-"+req.Zone),
				fmt.Sprintf("flow:%s:drain", req.Zone), req.LogicalTime); err != nil {
				return nil, err
			}
			flow, err := arbitration.DrainFlow(sess.VolumeMM3, req.DrainDurationMS)
			if err != nil {
				return nil, err
			}
			if flow < s.snapshot.WaterTest.MinDrainFlow {
				return nil, domain.NewError(domain.CodeInvalidArgument, "drain flow below minimum")
			}
			sess.Readings = append(sess.Readings, arbitration.WaterReading{Kind: "FLOW", Value: flow, LogicalTime: req.LogicalTime, Valid: true})
			sess.Phase = arbitration.WaterPhaseEmpty
		case arbitration.WaterPhaseEmpty:
			if sess.Phase != arbitration.WaterPhaseEmpty {
				return nil, domain.NewError(domain.CodeStageOutOfOrder, "empty out of phase")
			}
			sess.Phase = arbitration.WaterPhaseComplete
			sess.DrainedOK = true
			sess.BarrierOpen = true
		default:
			return nil, domain.NewError(domain.CodeInvalidArgument, "unknown water test phase")
		}
		st.appendEvent("WATER_TEST_ADVANCE", fmt.Sprintf("%s %s", req.Zone, req.Phase))
		return jsonResult(WaterTestResult{
			Zone:        req.Zone,
			Phase:       sess.Phase,
			VolumeMM3:   sess.VolumeMM3,
			DrainedOK:   sess.DrainedOK,
			BarrierOpen: sess.BarrierOpen,
		})
	})
	if err != nil {
		return nil, err
	}
	var out WaterTestResult
	if err := jsonUnmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// zoneVolume sums the fixed-point pipe volumes of every segment in a zone.
func (s *Service) zoneVolume(st *TaskState, zone domain.ZoneID) (int64, error) {
	var total int64
	for _, seg := range st.Task.Graph.Segments {
		if seg.Zone != zone {
			continue
		}
		v, err := arbitration.PipeVolumeMM3(int64(seg.Diameter), int64(seg.LengthMM))
		if err != nil {
			return 0, err
		}
		total, err = domain.SafeAdd(total, v)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}
