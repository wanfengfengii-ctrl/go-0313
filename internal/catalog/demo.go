package catalog

import "siphonic-roof-drainage-overflow-release/internal/domain"

// DemoSnapshot returns a representative, immutable rule snapshot used by the
// executable entry point so the server runs end-to-end without an external
// configuration source. Production deployments would load a persisted
// summary; this keeps the demo deterministic and self-contained.
func DemoSnapshot() *RuleSnapshot {
	return &RuleSnapshot{
		Version:      1,
		BuildingZone: "TERMINAL-A",
		DiameterTransitions: map[domain.DiameterMM][]domain.DiameterMM{
			110: {110, 160},
			160: {160, 200},
			200: {200, 250},
			250: {250, 315},
			315: {315},
		},
		EnvWindow:  EnvWindow{MinTemperature: -10, MaxTemperature: 60, MinHumidity: 0, MaxHumidity: 100},
		Pressure:   PressureCurve{MinIntegral: 1, MaxIntegral: 1 << 40, MaxSwitchoverMS: 10000, MinCoolingMS: 100},
		Bead:       BeadWindow{MinMM: 1, MaxMM: 30},
		Detection:  DetectionProgram{BorescopeRequired: true, AppearanceRequired: true},
		WaterTest:  WaterTestProgram{HoldMS: 1000, MinDrainFlow: 1, MinWaterLevel: 1},
		RetryLimit: 3,
	}
}
