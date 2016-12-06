package dynamics

import (
	"fmt"
	"math"

	"github.com/gonum/floats"
)

const (
	eps = 1e-3
)

func floatEqual(a, b float64) (bool, error) {
	if !floats.EqualWithinRel(a, b, eps) {
		return false, fmt.Errorf("difference of %3.10f", math.Abs(a-b))
	}
	return true, nil
}

func vectorsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := len(a) - 1; i >= 0; i-- {
		if !floats.EqualWithinRel(a[i], b[i], eps) {
			return false
		}
	}
	return true
}

//anglesEqual returns whether two angles in Radians are equal.
func anglesEqual(a, b float64) (bool, error) {
	diff := math.Mod(math.Abs(a-b), 2*math.Pi)
	if diff < angleε {
		return true, nil
	}
	return false, fmt.Errorf("difference of %3.10f degrees", math.Abs(Rad2deg(diff)))
}
