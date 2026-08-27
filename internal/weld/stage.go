// Package weld models the per-weld hot-melt stage state machine and the
// evidence recorded for each stage. Only a contiguous prefix from trimming
// through cooling may become effective evidence.
package weld

import (
	"fmt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// Stage is one step of the nine-step hot-melt sequence.
type Stage int

const (
	StageTrimming   Stage = iota // 切平
	StageFacing                  // 刨削
	StageCleaning                // 清洁
	StageAlignment               // 对中
	StageHeating                 // 加热翻边
	StageSwitchover              // 切换
	StagePressurize              // 加压
	StageHold                    // 保压
	StageCooling                 // 冷却
)

// StageNames maps each stage to its Chinese label for stable serialisation.
var StageNames = [...]string{
	"切平", "刨削", "清洁", "对中", "加热翻边", "切换", "加压", "保压", "冷却",
}

// String returns the Chinese stage label.
func (s Stage) String() string {
	if s < 0 || int(s) >= len(StageNames) {
		return fmt.Sprintf("Stage(%d)", int(s))
	}
	return StageNames[s]
}

// Valid returns true for a stage in the closed nine-stage sequence.
func (s Stage) Valid() bool {
	return s >= StageTrimming && s <= StageCooling
}

// Prefix is the ordered, gap-free sequence of stages that have effective
// evidence. Its length is the current effective prefix length.
type Prefix struct {
	Stages []Stage
}

// CanAppend reports whether stage s may legally follow the current prefix:
// it must be the immediate successor and the prefix must be contiguous.
func (p Prefix) CanAppend(s Stage) bool {
	if !s.Valid() {
		return false
	}
	if len(p.Stages) == 0 {
		return s == StageTrimming
	}
	last := p.Stages[len(p.Stages)-1]
	return s == last+1
}

// Append returns a new prefix extended with s, or an error if s is out of
// order. It never mutates the receiver, so a rejected stage cannot corrupt
// the effective prefix.
func (p Prefix) Append(s Stage) (Prefix, error) {
	if !p.CanAppend(s) {
		return p, domain.NewError(domain.CodeStageOutOfOrder,
			fmt.Sprintf("stage %s cannot follow prefix of length %d", s, len(p.Stages)))
	}
	next := make([]Stage, len(p.Stages)+1)
	copy(next, p.Stages)
	next[len(p.Stages)] = s
	return Prefix{Stages: next}, nil
}

// Effective returns the stages that currently have effective evidence.
func (p Prefix) Effective() []Stage {
	out := make([]Stage, len(p.Stages))
	copy(out, p.Stages)
	return out
}
