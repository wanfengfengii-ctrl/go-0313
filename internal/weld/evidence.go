package weld

import "siphonic-roof-drainage-overflow-release/internal/domain"

// WeldEvidence is the recorded trajectory of a single weld: the two bound
// ports, the machine and clamp used, the effective stage prefix, the
// temperature, bead, switchover, pressure and cooling records, and the
// appearance and borescope results. Superseded generations keep their
// evidence for audit but are never the current conclusion source.
type WeldEvidence struct {
	Weld             domain.WeldID
	Generation       domain.Generation
	PortA            domain.PortID
	PortB            domain.PortID
	Machine          domain.ResourceID
	Clamp            domain.ResourceID
	Prefix           Prefix
	LogicalTimes     []int64
	Temperatures     []int64
	Beads            []int64
	SwitchoverMS     []int64
	PressurePoints   []int64
	PressureIntegral int64
	CoolingRecords   []int64
	Appearance       string
	Borescope        string
	HangerOK         bool
	FixedNodeOK      bool
	Installed        bool
	Valid            bool
	Supersedes       domain.WeldID
}

// ResultClass classifies a device attempt outcome.
type ResultClass string

const (
	ResultSuccess    ResultClass = "SUCCESS"
	ResultRejected   ResultClass = "REJECTED"
	ResultDisconnect ResultClass = "DISCONNECTED"
	ResultTimeout    ResultClass = "TIMEOUT"
	ResultCalExpired ResultClass = "CALIBRATION_EXPIRED"
	ResultMalformed  ResultClass = "MALFORMED"
)

// DeviceAttempt is the audit trail of a scripted device call. Failures only
// append attempts with a deterministic retry sequence and logical time; they
// never produce a reading or advance a stage.
type DeviceAttempt struct {
	DeviceType  string
	ScriptKey   string
	LogicalTime int64
	Attempt     domain.AttemptIndex
	ResultClass ResultClass
	Reading     int64
	Retryable   bool
	RetryLimit  int
}
