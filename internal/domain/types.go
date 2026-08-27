package domain

// Typed identifiers keep the append-only aggregate model self-describing and
// prevent accidental cross-entity assignment. Every identity below is stable
// and monotonic for the lifetime of a task snapshot.

type TaskID string
type ZoneID string
type DrainID string
type SegmentID string
type FittingID string
type PortID string
type OutletID string
type MaterialID string
type BatchID string
type WeldID string
type Generation int64
type ResourceID string
type OperationID string
type AttemptIndex int64

// LengthMM is a physical length measured in whole millimetres. The domain
// never stores fractional millimetres: every cut, remainder, sample, removed
// segment and loss is an exact integer, which is what makes the material
// conservation invariant checkable.
type LengthMM int64

// DiameterMM is a pipe outer diameter in whole millimetres. Diameter
// transitions are validated against the rule snapshot's allowed table, so
// illegal step changes are rejected before any topology is committed.
type DiameterMM int64

// MaterialKind classifies a lineage node. It is closed: every disposition
// must map to exactly one kind so conservation sums stay well-defined.
type MaterialKind string

const (
	KindPipe    MaterialKind = "PIPE"
	KindFitting MaterialKind = "FITTING"
	KindStub    MaterialKind = "STUB"
	KindSample  MaterialKind = "SAMPLE"
	KindRemoved MaterialKind = "REMOVED"
	KindLoss    MaterialKind = "LOSS"
)

// Disposition records where a lineage node currently stands. A node has
// exactly one active disposition at a time.
type Disposition string

const (
	DispositionAvailable Disposition = "AVAILABLE"
	DispositionInstalled Disposition = "INSTALLED"
	DispositionSample    Disposition = "SAMPLE"
	DispositionRemoved   Disposition = "REMOVED"
	DispositionLoss      Disposition = "LOSS"
)
