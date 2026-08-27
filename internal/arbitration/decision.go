// Package arbitration models the water-test and final-decision boundary: zone
// water test sessions, the two-person independent review, and the single-write
// final decision that mints the unique temporary overflow release credential.
package arbitration

import "siphonic-roof-drainage-overflow-release/internal/domain"

// FinalType is the single-write terminal outcome for a task.
type FinalType string

const (
	FinalNone      FinalType = "NONE"
	FinalAdmission FinalType = "ADMISSION" // 准入：签发临时溢流导排拆除凭据
	FinalIsolation FinalType = "ISOLATION" // 渗漏风险隔离
	FinalCancelled FinalType = "CANCELLED" // 取消
)

// Reviewer is a person with an active qualification.
type Reviewer struct {
	ID         string
	Qualified  bool
	QualExpiry int64 // logical time
}

// Review is one independent signature. Two different, currently qualified
// reviewers must sign before a final decision can be admitted.
type Review struct {
	Task        domain.TaskID
	Reviewer    string
	Signature   string
	LogicalTime int64
}

// WaterTestPhase is one step of the zone water-test timeline.
type WaterTestPhase string

const (
	WaterPhaseIdle     WaterTestPhase = "IDLE"
	WaterPhaseFill     WaterTestPhase = "FILL"
	WaterPhaseHold     WaterTestPhase = "HOLD"
	WaterPhaseDrain    WaterTestPhase = "DRAIN"
	WaterPhaseEmpty    WaterTestPhase = "EMPTY"
	WaterPhaseComplete WaterTestPhase = "COMPLETE"
)

// WaterTestSession records the zone water-test timeline: fill, hold, drain
// and empty, with integer water-level and flow readings and the computed pipe
// volume.
type WaterTestSession struct {
	Task        domain.TaskID
	Zone        domain.ZoneID
	Phase       WaterTestPhase
	VolumeMM3   int64
	FillTime    int64
	Readings    []WaterReading
	DrainedOK   bool
	BarrierOpen bool
}

// WaterReading is a single deterministic reading from a scripted gauge.
type WaterReading struct {
	Kind        string // WATER_LEVEL or FLOW
	Value       int64
	LogicalTime int64
	Valid       bool
}

// FinalDecision is the terminal, non-overridable outcome. Once written, no
// other decision may overwrite it, enforced by a compare-and-swap on the
// task's final barrier version.
type FinalDecision struct {
	Task           domain.TaskID
	Type           FinalType
	Credential     string // unique, only present for admission
	Reviews        []Review
	BarrierVersion int64
}
