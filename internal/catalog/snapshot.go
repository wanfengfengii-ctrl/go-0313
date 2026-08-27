// Package catalog is the siphonic network and hot-melt process rule
// directory. It holds the versioned design and process summaries, validates
// integer geometry, hydraulic direction, diameter transitions, acyclicity,
// unique reachability, the hot-melt program and inspection thresholds, and
// hands the locked task an immutable rule snapshot.
package catalog

import "siphonic-roof-drainage-overflow-release/internal/domain"

// SummaryVersion is the monotonic version of a design or process summary.
type SummaryVersion int64

// EnvWindow is the acceptable environment window for a hot-melt weld.
type EnvWindow struct {
	MinTemperature int64
	MaxTemperature int64
	MinHumidity    int64
	MaxHumidity    int64
}

// PressureCurve bounds the pressurise/hold phases.
type PressureCurve struct {
	MinIntegral     int64
	MaxIntegral     int64
	MaxSwitchoverMS int64
	MinCoolingMS    int64
}

// DetectionProgram maps inspection kinds to their thresholds.
type DetectionProgram struct {
	BorescopeRequired  bool
	AppearanceRequired bool
}

// WaterTestProgram defines the zone water test procedure and thresholds.
type WaterTestProgram struct {
	HoldMS        int64
	MinDrainFlow  int64
	MinWaterLevel int64
}

// RuleSnapshot is the immutable set of rules a task locks against. Once a
// task is locked, this snapshot cannot change in place; a newer summary
// version requires a new task.
type RuleSnapshot struct {
	Version             SummaryVersion
	BuildingZone        string
	DiameterTransitions map[domain.DiameterMM][]domain.DiameterMM
	EnvWindow           EnvWindow
	Pressure            PressureCurve
	Bead                BeadWindow
	Detection           DetectionProgram
	WaterTest           WaterTestProgram
	RetryLimit          int
}

// BeadWindow bounds the melt bead (翻边) height produced during heating. A
// bead outside this window indicates a pressure or temperature fault and is
// rejected before the stage prefix advances.
type BeadWindow struct {
	MinMM int64
	MaxMM int64
}

// AllowsTransition reports whether a diameter step from -> to is legal under
// this snapshot's transition table.
func (r *RuleSnapshot) AllowsTransition(from, to domain.DiameterMM) bool {
	for _, d := range r.DiameterTransitions[from] {
		if d == to {
			return true
		}
	}
	return false
}
