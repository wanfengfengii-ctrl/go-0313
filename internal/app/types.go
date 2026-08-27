// Package app is the application service that orchestrates every business
// flow: task creation and locking, material lineage and leases, hot-melt
// evidence, water tests, anomaly propagation and repairs, two-person review
// and the single-write final decision. It holds the in-memory working set,
// persists full snapshots transactionally, and enforces Operation-Id
// idempotency across the whole command surface.
package app

import (
	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/topology"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

// Event is an append-only audit event. Its sequence number is monotonic for
// the lifetime of a task and never reused.
type Event struct {
	Seq    int64  `json:"seq"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// AnomalyKind is the closed set of detectable defects that can force a
// cut-out and re-weld.
type AnomalyKind string

const (
	AnomalyWrongDiameter      AnomalyKind = "WRONG_DIAMETER"
	AnomalyContamination      AnomalyKind = "CONTAMINATION"
	AnomalyAlignment          AnomalyKind = "ALIGNMENT"
	AnomalyBead               AnomalyKind = "BEAD"
	AnomalySwitchoverTimeout  AnomalyKind = "SWITCHOVER_TIMEOUT"
	AnomalyPressureGap        AnomalyKind = "PRESSURE_GAP"
	AnomalyCoolingDisturbance AnomalyKind = "COOLING_DISTURBANCE"
	AnomalyNecking            AnomalyKind = "NECKING"
	AnomalyLeak               AnomalyKind = "LEAK"
	AnomalySupportShift       AnomalyKind = "SUPPORT_SHIFT"
	AnomalyDrainInsufficient  AnomalyKind = "DRAIN_INSUFFICIENT"
)

// Anomaly is a detected defect. It references the weld or zone it was found
// on and carries a deterministic detail string.
type Anomaly struct {
	ID     string        `json:"id"`
	Kind   AnomalyKind   `json:"kind"`
	Weld   domain.WeldID `json:"weld,omitempty"`
	Zone   domain.ZoneID `json:"zone,omitempty"`
	Detail string        `json:"detail"`
}

// RepairItem is one cut-out / re-weld entry in a repair set.
type RepairItem struct {
	Weld       domain.WeldID     `json:"weld"`
	Generation domain.Generation `json:"generation"`
	Reason     string            `json:"reason"`
}

// RepairSet is the deterministic, sorted set of welds to cut out and re-weld
// for a set of anomalies. Its key is derived from the anomaly identity and
// task generation.
type RepairSet struct {
	ID    string       `json:"id"`
	Key   string       `json:"key"`
	Items []RepairItem `json:"items"`
}

// MaterialSpec is a root material node submitted at lock time.
type MaterialSpec struct {
	ID     domain.MaterialID   `json:"id"`
	Batch  domain.BatchID      `json:"batch"`
	Kind   domain.MaterialKind `json:"kind"`
	Length domain.LengthMM     `json:"length_mm"`
}

// TaskState is the full aggregate state of one construction task. It is the
// unit of snapshot persistence and is rebuilt on restart.
type TaskState struct {
	Task       topology.TaskAggregate                          `json:"task"`
	Lineage    *lineage.MaterialLineage                        `json:"lineage"`
	Leases     []lineage.Lease                                 `json:"leases"`
	Welds      map[domain.WeldID][]weld.WeldEvidence           `json:"welds"`
	CurrentGen map[domain.WeldID]domain.Generation             `json:"current_gen"`
	WaterTests map[domain.ZoneID]*arbitration.WaterTestSession `json:"water_tests"`
	Anomalies  []Anomaly                                       `json:"anomalies"`
	RepairSets []RepairSet                                     `json:"repair_sets"`
	Reviews    []arbitration.Review                            `json:"reviews"`
	Final      *arbitration.FinalDecision                      `json:"final"`
	Attempts   []weld.DeviceAttempt                            `json:"attempts"`
	Events     []Event                                         `json:"events"`
	// PortBindings records the current material identity bound to each port.
	// A port belongs to at most one current material at a time.
	PortBindings map[domain.PortID]domain.MaterialID `json:"port_bindings"`
}

// NewTaskState builds an empty task state bound to the given task id.
func NewTaskState(id domain.TaskID) *TaskState {
	return &TaskState{
		Task:         topology.TaskAggregate{ID: id, Generation: 1, LockState: topology.LockStateDraft},
		Lineage:      lineage.NewLineage(),
		Welds:        make(map[domain.WeldID][]weld.WeldEvidence),
		CurrentGen:   make(map[domain.WeldID]domain.Generation),
		WaterTests:   make(map[domain.ZoneID]*arbitration.WaterTestSession),
		PortBindings: make(map[domain.PortID]domain.MaterialID),
	}
}

// currentWeld returns the current-generation evidence for a weld, or false.
func (s *TaskState) currentWeld(id domain.WeldID) (weld.WeldEvidence, bool) {
	gen, ok := s.CurrentGen[id]
	if !ok {
		return weld.WeldEvidence{}, false
	}
	for _, ev := range s.Welds[id] {
		if ev.Generation == gen {
			return ev, true
		}
	}
	return weld.WeldEvidence{}, false
}

// appendEvent records an audit event with a fresh sequence number.
func (s *TaskState) appendEvent(kind, detail string) {
	s.Task.EventSeq++
	s.Events = append(s.Events, Event{Seq: s.Task.EventSeq, Kind: kind, Detail: detail})
}
