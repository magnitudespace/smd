package dynamics

import (
	"fmt"
	"math"

	"github.com/gonum/matrix/mat64"
)

const (
	oneQuarter = 1 / 4.0
)

var (
	eye = mat64.NewDense(3, 3, []float64{1, 0, 0, 0, 0, 0, 0, 0, 1})
)

/*-----*/
/* Modified Rodrigez Parameters */
/*-----*/

// MRP defines Modified Rodrigez Parameters.
type MRP struct {
	s1, s2, s3 float64
}

// Equals returns true if both MRPs correspond to the same attitude.
func (s *MRP) Equals(o *MRP) bool {
	const (
		relError = 1e-12
	)
	if math.Abs(s.s1-o.s1) < relError &&
		math.Abs(s.s2-o.s2) < relError &&
		math.Abs(s.s3-o.s3) < relError {
		return true
	}
	oc := MRP{o.s1, o.s2, o.s3}
	oc.Short() // Create a local short copy.
	if math.Abs(s.s1-oc.s1) < relError &&
		math.Abs(s.s2-oc.s2) < relError &&
		math.Abs(s.s3-oc.s3) < relError {
		return true
	}
	return false
}

func (s *MRP) squared() float64 {
	return s.s1*s.s1 + s.s2*s.s2 + s.s3*s.s3
}

func (s *MRP) norm() float64 {
	return math.Sqrt(s.squared())
}

// Short refreshes this MRP representation to use its short notation.
func (s *MRP) Short() {
	if s.norm() > 1 {
		sq := s.squared()
		// Switch to shadow set.
		s.s1 = -s.s1 / sq
		s.s2 = -s.s2 / sq
		s.s3 = -s.s3 / sq
	}
}

// Tilde returns the tilde matrix of this MRP.
// The m parameter allows to multiply directly the Tilde matrix.
func (s *MRP) Tilde(m float64) *mat64.Dense {
	return mat64.NewDense(3, 3, []float64{0, -s.s3 * m, s.s2 * m,
		s.s3 * m, 0, -s.s1 * m,
		-s.s2 * m, s.s1 * m, 0})
}

// OuterProduct returns the outer product of this MRP with itself.
// The m parameter allows to multiply directly the outer product with a scalar.
func (s *MRP) OuterProduct(m float64) *mat64.Dense {
	return mat64.NewDense(3, 3, []float64{
		s.s1 * s.s1, s.s1 * s.s2, s.s1 * s.s3,
		s.s2 * s.s1, s.s2 * s.s2, s.s2 * s.s3,
		s.s3 * s.s1, s.s3 * s.s2, s.s3 * s.s3,
	})
}

// B returns the B matrix for MRP computations.
func (s *MRP) B() *mat64.Dense {
	B := mat64.NewDense(3, 3, nil)
	e1 := mat64.NewDense(3, 3, []float64{1 - s.squared(), 0, 0,
		0, 1 - s.squared(), 0,
		0, 0, 1 - s.squared()})
	e2 := s.Tilde(2)
	B.Add(e1, e2)
	fmt.Printf("B = %+v\n", B)
	B.Add(B, s.OuterProduct(2))
	fmt.Printf("B = %+v\n", B)
	return B
}

func (s *MRP) String() string {
	return fmt.Sprintf("[%1.5f  %1.5f  %1.5f]", s.s1, s.s2, s.s3)
}

// Attitude defines an attitude with an orientation, an angular velocity and an inertial tensor.
// *ALMOST* implements rk4.Integrable.
type Attitude struct {
	Attitude      *MRP
	Velocity      *mat64.Vector
	InertiaTensor *mat64.Dense
	initAngMom    float64 // Initial angular moment (integrator failsafe)
	mf1, mf2, mf3 float64 // Inertial tensor ratios
}

// NewAttitude returns an Attitude pointer.
func NewAttitude(sigma [3]float64, omega [3]float64, tensor []float64) *Attitude {
	a := Attitude{}
	a.Attitude = &MRP{sigma[0], sigma[1], sigma[2]}
	a.Velocity = mat64.NewVector(3, []float64{omega[0], omega[1], omega[2]})
	a.InertiaTensor = mat64.NewDense(3, 3, tensor)
	a.mf1 = (a.InertiaTensor.At(1, 1) - a.InertiaTensor.At(2, 2)) / a.InertiaTensor.At(0, 0)
	a.mf2 = (a.InertiaTensor.At(2, 2) - a.InertiaTensor.At(0, 0)) / a.InertiaTensor.At(1, 1)
	a.mf3 = (a.InertiaTensor.At(0, 0) - a.InertiaTensor.At(1, 1)) / a.InertiaTensor.At(2, 2)
	a.initAngMom = a.Momentum()
	return &a
}

// Momentum returns the angular moment of this body.
func (a *Attitude) Momentum() float64 {
	mom := mat64.Dense{}
	mom.Mul(a.InertiaTensor, a.Velocity)
	return mat64.Norm(&mom, 1)
}

// GetState returns the state of this attitude for the EOM as defined below.
func (a *Attitude) GetState() []float64 {
	return []float64{a.Attitude.s1, a.Attitude.s2, a.Attitude.s3, a.Velocity.At(0, 0), a.Velocity.At(1, 0), a.Velocity.At(2, 0)}
}

// SetState returns the state of this attitude for the EOM as defined below.
func (a *Attitude) SetState(i uint64, s []float64) {
	a.Attitude.s1 = s[0]
	a.Attitude.s2 = s[1]
	a.Attitude.s3 = s[2]
	a.Velocity.SetVec(0, s[3])
	a.Velocity.SetVec(1, s[4])
	a.Velocity.SetVec(2, s[5])
	fmt.Printf("state = %+v", a.GetState())
	// Integration failsafe
	if diff := math.Abs(a.Momentum() - a.initAngMom); diff > -1e10 {
		panic(fmt.Errorf("total momentum of body changed at i=%d: %5.5f", i, diff))
	}
}

// Func is the integrator function.
func (a *Attitude) Func(t float64, s []float64) []float64 {
	fmt.Printf("state = %+v\n", s)
	sigma := MRP{s[0], s[1], s[2]}
	omega := mat64.NewVector(3, []float64{s[3], s[4], s[5]})
	omega.MulVec(sigma.B(), omega)
	f := make([]float64, 6)
	f[0] = oneQuarter * omega.At(0, 0)
	f[1] = oneQuarter * omega.At(1, 0)
	f[2] = oneQuarter * omega.At(2, 0)
	f[3] = a.mf1 * omega.At(1, 0) * omega.At(2, 0)
	f[4] = a.mf2 * omega.At(0, 0) * omega.At(2, 0)
	f[5] = a.mf3 * omega.At(1, 0) * omega.At(0, 0)
	fmt.Printf("f = %+v\n", f)
	return f
}
