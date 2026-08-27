package weld_test

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestPrefixFullNineStageSequence(t *testing.T) {
	var p weld.Prefix
	for i := weld.StageTrimming; i <= weld.StageCooling; i++ {
		next, err := p.Append(i)
		if err != nil {
			t.Fatalf("stage %s rejected in sequence: %v", i, err)
		}
		p = next
	}
	if got := len(p.Effective()); got != 9 {
		t.Fatalf("effective prefix length = %d, want 9", got)
	}
}

func TestPrefixSkipStageRejected(t *testing.T) {
	var p weld.Prefix
	// Starting at the wrong stage (not trimming) is rejected.
	if _, err := p.Append(weld.StageFacing); err == nil {
		t.Fatal("expected first stage must be trimming")
	} else if se, ok := err.(*domain.StableError); !ok || se.Code != domain.CodeStageOutOfOrder {
		t.Fatalf("expected CodeStageOutOfOrder, got %v", err)
	}
}

func TestPrefixDuplicateStageRejected(t *testing.T) {
	p, err := (weld.Prefix{}).Append(weld.StageTrimming)
	if err != nil {
		t.Fatalf("append trimming: %v", err)
	}
	if _, err := p.Append(weld.StageTrimming); err == nil {
		t.Fatal("expected duplicate stage rejection")
	}
}

func TestPrefixJumpRejected(t *testing.T) {
	p, err := (weld.Prefix{}).Append(weld.StageTrimming)
	if err != nil {
		t.Fatalf("append trimming: %v", err)
	}
	if _, err := p.Append(weld.StageCleaning); err == nil {
		t.Fatal("expected out-of-order jump rejection")
	}
}

func TestPrefixInvalidStageRejected(t *testing.T) {
	if _, err := (weld.Prefix{}).Append(weld.Stage(99)); err == nil {
		t.Fatal("expected invalid stage rejection")
	}
}

func TestStageNamesCoverage(t *testing.T) {
	if weld.StageTrimming.String() != "切平" {
		t.Fatalf("StageTrimming.String() = %q", weld.StageTrimming.String())
	}
	if weld.StageCooling.String() != "冷却" {
		t.Fatalf("StageCooling.String() = %q", weld.StageCooling.String())
	}
}
