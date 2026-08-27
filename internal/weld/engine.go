package weld

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// StageSubmission carries the evidence for one hot-melt stage attempt. Every
// field is an integer or fixed-point integer so the outcome is deterministic.
type StageSubmission struct {
	Stage        Stage
	Machine      domain.ResourceID
	Clamp        domain.ResourceID
	Generation   domain.Generation
	PortA        domain.PortID
	PortB        domain.PortID
	Temperature  int64
	Humidity     int64
	BeadMM       int64
	SwitchoverMS int64
	Pressures    []int64
	CoolingMS    int64
	LogicalTime  int64
}

// Engine validates stage submissions against an immutable rule snapshot and
// advances the effective stage prefix. Only a contiguous prefix from trimming
// through cooling may become effective evidence; every rejected submission
// leaves the prefix untouched.
type Engine struct {
	Snapshot *catalog.RuleSnapshot
}

// Apply validates sub against ev and, when legal, appends the stage and its
// evidence to ev. It returns a stable error for out-of-order stages, wrong
// machine or clamp, wrong generation, environment breach, bead breach,
// switchover timeout, pressure-integral gap, cooling shortfall or a logical
// clock that moves backwards.
func (e *Engine) Apply(ev *WeldEvidence, sub StageSubmission, now int64) error {
	if ev.Weld == "" {
		return domain.NewError(domain.CodeNotFound, "unknown weld")
	}
	if sub.Generation != ev.Generation {
		return domain.NewError(domain.CodeStageOutOfOrder,
			fmt.Sprintf("generation %d does not match current %d", sub.Generation, ev.Generation))
	}
	if ev.Machine != "" && sub.Machine != ev.Machine {
		return domain.NewError(domain.CodeInvalidArgument,
			fmt.Sprintf("machine %s does not match recorded %s", sub.Machine, ev.Machine))
	}
	if ev.Clamp != "" && sub.Clamp != ev.Clamp {
		return domain.NewError(domain.CodeInvalidArgument,
			fmt.Sprintf("clamp %s does not match recorded %s", sub.Clamp, ev.Clamp))
	}
	if sub.PortA == "" || sub.PortB == "" || sub.PortA == sub.PortB {
		return domain.NewError(domain.CodeInvalidArgument, "weld requires two distinct ports")
	}
	if ev.PortA == "" {
		ev.PortA, ev.PortB = sub.PortA, sub.PortB
	} else if ev.PortA != sub.PortA || ev.PortB != sub.PortB {
		return domain.NewError(domain.CodeDuplicatePort, "weld ports differ from recorded ports")
	}
	if sub.LogicalTime <= ev.LastLogicalTime() {
		return domain.NewError(domain.CodeStageOutOfOrder, "logical time moved backwards")
	}

	if err := e.validateEnvironment(sub); err != nil {
		return err
	}
	if err := e.validateStageEvidence(sub); err != nil {
		return err
	}

	next, err := ev.Prefix.Append(sub.Stage)
	if err != nil {
		return err
	}
	ev.Prefix = next
	ev.Machine = sub.Machine
	ev.Clamp = sub.Clamp
	ev.LogicalTimes = append(ev.LogicalTimes, sub.LogicalTime)
	ev.Temperatures = append(ev.Temperatures, sub.Temperature)
	ev.Beads = append(ev.Beads, sub.BeadMM)
	ev.SwitchoverMS = append(ev.SwitchoverMS, sub.SwitchoverMS)
	if len(sub.Pressures) > 0 {
		ev.PressurePoints = append(ev.PressurePoints, sub.Pressures...)
	}
	if sub.Stage == StageCooling {
		ev.CoolingRecords = append(ev.CoolingRecords, sub.CoolingMS)
	}
	ev.Valid = len(ev.Prefix.Stages) == 9
	return nil
}

func (e *Engine) validateEnvironment(sub StageSubmission) error {
	w := e.Snapshot.EnvWindow
	if sub.Temperature < w.MinTemperature || sub.Temperature > w.MaxTemperature {
		return domain.NewError(domain.CodeStageOutOfOrder,
			fmt.Sprintf("temperature %d outside window [%d,%d]", sub.Temperature, w.MinTemperature, w.MaxTemperature))
	}
	if sub.Humidity < w.MinHumidity || sub.Humidity > w.MaxHumidity {
		return domain.NewError(domain.CodeStageOutOfOrder,
			fmt.Sprintf("humidity %d outside window [%d,%d]", sub.Humidity, w.MinHumidity, w.MaxHumidity))
	}
	return nil
}

func (e *Engine) validateStageEvidence(sub StageSubmission) error {
	switch sub.Stage {
	case StageHeating:
		b := e.Snapshot.Bead
		if sub.BeadMM < b.MinMM || sub.BeadMM > b.MaxMM {
			return domain.NewError(domain.CodeStageOutOfOrder,
				fmt.Sprintf("bead %d outside window [%d,%d]", sub.BeadMM, b.MinMM, b.MaxMM))
		}
	case StageSwitchover:
		if sub.SwitchoverMS > e.Snapshot.Pressure.MaxSwitchoverMS {
			return domain.NewError(domain.CodeStageOutOfOrder,
				fmt.Sprintf("switchover %d exceeds limit %d", sub.SwitchoverMS, e.Snapshot.Pressure.MaxSwitchoverMS))
		}
	case StagePressurize, StageHold:
		integral, err := arbitration.PressureIntegral(sub.Pressures)
		if err != nil {
			return err
		}
		p := e.Snapshot.Pressure
		if integral < p.MinIntegral || integral > p.MaxIntegral {
			return domain.NewError(domain.CodeStageOutOfOrder,
				fmt.Sprintf("pressure integral %d outside [%d,%d]", integral, p.MinIntegral, p.MaxIntegral))
		}
	case StageCooling:
		if sub.CoolingMS < e.Snapshot.Pressure.MinCoolingMS {
			return domain.NewError(domain.CodeStageOutOfOrder,
				fmt.Sprintf("cooling %d below minimum %d", sub.CoolingMS, e.Snapshot.Pressure.MinCoolingMS))
		}
	}
	return nil
}

// LastLogicalTime returns the most recent logical time recorded on the
// evidence, or zero when no stage has been applied.
func (ev *WeldEvidence) LastLogicalTime() int64 {
	if len(ev.LogicalTimes) == 0 {
		return 0
	}
	return ev.LogicalTimes[len(ev.LogicalTimes)-1]
}
