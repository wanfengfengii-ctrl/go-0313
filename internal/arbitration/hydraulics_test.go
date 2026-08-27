package arbitration

import (
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

func TestPipeVolumePositive(t *testing.T) {
	v, err := PipeVolumeMM3(110, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if v <= 0 {
		t.Fatalf("expected positive volume, got %d", v)
	}
	// Doubling length must double volume exactly (linear in L).
	v2, _ := PipeVolumeMM3(110, 2000)
	if v2 != v*2 {
		t.Fatalf("volume not linear in length: %d vs %d", v2, v*2)
	}
}

func TestPipeVolumeDegenerate(t *testing.T) {
	for _, d := range [][2]int64{{0, 1000}, {110, 0}, {-5, 1000}} {
		if _, err := PipeVolumeMM3(d[0], d[1]); err == nil || err.(*domain.StableError).Code != domain.CodeDegenerate {
			t.Fatalf("expected DEGENERATE for %v, got %v", d, err)
		}
	}
}

func TestPipeVolumeOverflow(t *testing.T) {
	if _, err := PipeVolumeMM3(1<<40, 1<<40); err == nil || err.(*domain.StableError).Code != domain.CodeOverflow {
		t.Fatalf("expected OVERFLOW, got %v", err)
	}
}

func TestDrainFlowRounding(t *testing.T) {
	if v, err := DrainFlow(1000, 10); err != nil || v != 100 {
		t.Fatalf("expected 100, got %d err %v", v, err)
	}
	// 10/3 rounds half-away-from-zero to 3.
	if v, err := DrainFlow(10, 3); err != nil || v != 3 {
		t.Fatalf("expected 3, got %d err %v", v, err)
	}
	if _, err := DrainFlow(1000, 0); err == nil || err.(*domain.StableError).Code != domain.CodeDivideByZero {
		t.Fatalf("expected DIVIDE_BY_ZERO, got %v", err)
	}
}

func TestPressureIntegral(t *testing.T) {
	if v, err := PressureIntegral([]int64{100, 200}); err != nil || v != 150 {
		t.Fatalf("expected 150, got %d err %v", v, err)
	}
	if _, err := PressureIntegral([]int64{100}); err == nil {
		t.Fatal("expected error for single point")
	}
}
