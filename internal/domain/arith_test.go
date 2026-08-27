package domain_test

import (
	"math"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

func TestSafeAdd(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
		err  bool
	}{
		{"positive", 40, 2, 42, false},
		{"negative", -5, -7, -12, false},
		{"zero", 0, 9, 9, false},
		{"max overflow", math.MaxInt64, 1, 0, true},
		{"min overflow", math.MinInt64, -1, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := domain.SafeAdd(c.a, c.b)
			if c.err {
				if err == nil {
					t.Fatalf("expected overflow error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("SafeAdd(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestSafeSub(t *testing.T) {
	if got, err := domain.SafeSub(10, 3); err != nil || got != 7 {
		t.Fatalf("SafeSub(10,3) = %d, %v; want 7", got, err)
	}
	if _, err := domain.SafeSub(math.MinInt64, 1); err == nil {
		t.Fatal("expected overflow for MinInt64 - 1")
	}
}

func TestSafeMul(t *testing.T) {
	if got, err := domain.SafeMul(6, 7); err != nil || got != 42 {
		t.Fatalf("SafeMul(6,7) = %d, %v; want 42", got, err)
	}
	if _, err := domain.SafeMul(math.MaxInt64/2, 3); err == nil {
		t.Fatal("expected overflow for large product")
	}
	if _, err := domain.SafeMul(math.MinInt64, -1); err == nil {
		t.Fatal("expected overflow for MinInt64 * -1")
	}
}

func TestDivRoundHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		a, b int64
		want int64
	}{
		{5, 2, 3},   // 2.5 -> 3
		{4, 2, 2},   // 2 -> 2
		{-5, 2, -3}, // -2.5 -> -3 (away from zero)
		{7, 3, 2},   // 2.333 -> 2
		{8, 3, 3},   // 2.667 -> 3
	}
	for _, c := range cases {
		got, err := domain.DivRound(c.a, c.b)
		if err != nil {
			t.Fatalf("DivRound(%d,%d) error: %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Fatalf("DivRound(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDivRoundByZero(t *testing.T) {
	if _, err := domain.DivRound(1, 0); err == nil {
		t.Fatal("expected divide-by-zero error")
	}
}

func TestMulDivVolume(t *testing.T) {
	// length * area where area approximates pi*d^2/4 via (pi*d*d)/(4) with
	// pi scaled to 31416/10000: volume = length * 31416 * d^2 / 40000.
	got, err := domain.MulDiv(1000, 31416*110*110, 40000)
	if err != nil {
		t.Fatalf("MulDiv error: %v", err)
	}
	if got <= 0 {
		t.Fatalf("expected positive volume, got %d", got)
	}
}

func TestMulDivOverflow(t *testing.T) {
	if _, err := domain.MulDiv(math.MaxInt64, math.MaxInt64, 1); err == nil {
		t.Fatal("expected overflow for MaxInt64 * MaxInt64")
	}
}
