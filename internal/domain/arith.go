// Package domain holds the stable, package-neutral primitives shared by every
// business package: overflow-checked integer arithmetic, deterministic
// fixed-point rounding, stable error protocol, and the typed identifiers and
// measurement units used throughout the quality-closure model.
package domain

import "math"

// Arithmetic errors are returned before any write to an aggregate, so a
// degenerate dimension, division by zero, or accumulated overflow never
// leaves a partial side effect.
var (
	ErrDivideByZero = &StableError{Code: CodeDivideByZero}
	ErrOverflow     = &StableError{Code: CodeOverflow}
)

// SafeAdd returns a+b or ErrOverflow when the result leaves the int64 range.
func SafeAdd(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// SafeSub returns a-b or ErrOverflow when the result leaves the int64 range.
func SafeSub(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrOverflow
	}
	return a - b, nil
}

// SafeMul returns a*b or ErrOverflow when the product leaves the int64 range.
func SafeMul(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	c := a * b
	if c/b != a {
		return 0, ErrOverflow
	}
	return c, nil
}

// DivRound returns a/b rounded half away from zero. It returns ErrDivideByZero
// when b is zero and ErrOverflow when the quotient leaves the int64 range.
// The half-away-from-zero rule is the documented, deterministic rounding for
// slopes, volumes, pressures, water levels and flow rates.
func DivRound(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	q := a / b
	r := a % b
	if r == 0 {
		return q, nil
	}
	ar, br := abs64(r), abs64(b)
	if 2*ar >= br {
		if (a < 0) != (b < 0) {
			q--
		} else {
			q++
		}
	}
	return q, nil
}

// MulDiv computes (a*b)/c using overflow-checked multiplication followed by
// half-away-from-zero division. It is the primitive behind pipe volume,
// pressure integrals and drainage flow: all three factors stay in int64 and
// every intermediate is checked before it is committed.
func MulDiv(a, b, c int64) (int64, error) {
	if c == 0 {
		return 0, ErrDivideByZero
	}
	p, err := SafeMul(a, b)
	if err != nil {
		return 0, err
	}
	return DivRound(p, c)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
