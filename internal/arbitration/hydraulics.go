package arbitration

import (
	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// piFixed is the fixed-point value of pi scaled by 10000. Using a rational
// approximation keeps every hydraulic quantity an integer and therefore
// deterministic and reproducible across platforms.
const piFixed int64 = 31416 // pi * 10000

// PipeVolumeMM3 computes the internal volume of a cylindrical pipe run in
// cubic millimetres using fixed-point pi and half-away-from-zero rounding:
//
//	V = pi * (d/2)^2 * L = (piFixed * d^2 * L) / (4 * scale)
//
// It returns a stable error for a degenerate (non-positive) diameter or
// length, division by zero (impossible here but kept for uniformity) or an
// accumulated overflow.
func PipeVolumeMM3(diameterMM, lengthMM int64) (int64, error) {
	if diameterMM <= 0 || lengthMM <= 0 {
		return 0, domain.NewError(domain.CodeDegenerate, "volume requires positive diameter and length")
	}
	d2, err := domain.SafeMul(diameterMM, diameterMM)
	if err != nil {
		return 0, err
	}
	num, err := domain.SafeMul(d2, lengthMM)
	if err != nil {
		return 0, err
	}
	num, err = domain.SafeMul(num, piFixed)
	if err != nil {
		return 0, err
	}
	// denominator = 4 * 10000
	den, err := domain.SafeMul(4, 10000)
	if err != nil {
		return 0, err
	}
	return domain.DivRound(num, den)
}

// PressureIntegral returns the trapezoidal area under a pressure point series
// using overflow-checked addition. It is shared with the weld engine and used
// by water-test pressure validation.
func PressureIntegral(points []int64) (int64, error) {
	if len(points) < 2 {
		return 0, domain.NewError(domain.CodeInvalidArgument, "pressure series needs at least two points")
	}
	var total int64
	for i := 1; i < len(points); i++ {
		sum, err := domain.SafeAdd(points[i-1], points[i])
		if err != nil {
			return 0, err
		}
		total, err = domain.SafeAdd(total, sum)
		if err != nil {
			return 0, err
		}
	}
	return domain.DivRound(total, 2)
}

// DrainFlow computes the mean drainage flow rate = volume / duration using
// half-away-from-zero rounding. It returns a stable error for division by
// zero (a zero-duration drain) and for overflow.
func DrainFlow(volumeMM3, durationMS int64) (int64, error) {
	if durationMS <= 0 {
		return 0, domain.NewError(domain.CodeDivideByZero, "drain duration must be positive")
	}
	return domain.DivRound(volumeMM3, durationMS)
}
