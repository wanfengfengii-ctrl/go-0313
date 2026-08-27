package topology

import "siphonic-roof-drainage-overflow-release/internal/domain"

// LockState is the task's locking status. Once locked, the snapshot and the
// graph are immutable.
type LockState string

const (
	LockStateDraft  LockState = "DRAFT"
	LockStateLocked LockState = "LOCKED"
)

// TaskAggregate is the root of the construction task: it tracks the current
// generation, a monotonic logical clock, the rule snapshot it was locked
// against, a final-decision barrier version and the append-only event
// sequence number used for idempotent replay on restart.
type TaskAggregate struct {
	ID           domain.TaskID
	Generation   domain.Generation
	LockState    LockState
	LogicalClock int64
	SnapshotID   string
	FinalVersion int64
	EventSeq     int64
	Graph        HydraulicGraph
}

// AdvanceClock bumps the logical clock and returns the new value. The clock
// never moves backwards, which is what makes out-of-order evidence
// detectable.
func (t *TaskAggregate) AdvanceClock() int64 {
	t.LogicalClock++
	return t.LogicalClock
}

// NextEventSeq reserves the next append-only event sequence number.
func (t *TaskAggregate) NextEventSeq() int64 {
	t.EventSeq++
	return t.EventSeq
}
