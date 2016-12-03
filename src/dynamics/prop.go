package dynamics

import (
	"fmt"
	"math"
)

// ControlLaw defines an enum of control laws.
type ControlLaw uint8

const (
	tangential ControlLaw = iota + 1
	antiTangential
	inversion
	coast
	optiΔa
	optiΔi
	optiΔe
	optiΔΩ
	optiΔω
	multiOpti
)

func (cl ControlLaw) String() string {
	switch cl {
	case tangential:
		return "tangential"
	case antiTangential:
		return "antiTangential"
	case inversion:
		return "inversion"
	case coast:
		return "coast"
	case optiΔa:
		return "optimal Δa"
	case optiΔe:
		return "optimal Δe"
	case optiΔi:
		return "optimal Δi"
	case optiΔΩ:
		return "optimal ΔΩ"
	case optiΔω:
		return "optimal Δω"
	case multiOpti:
		return "multiple optimized"
	}
	panic("cannot stringify unknown control law")
}

// ThrustControl defines a thrust control interface.
type ThrustControl interface {
	Control(o Orbit) []float64
	Type() ControlLaw
	Reason() string
}

// GenericCL partially defines a ThrustControl.
type GenericCL struct {
	reason string
	cl     ControlLaw
}

// Reason implements the ThrustControl interface.
func (cl GenericCL) Reason() string {
	return cl.reason
}

// Type implements the ThrustControl interface.
func (cl GenericCL) Type() ControlLaw {
	return cl.cl
}

// Thruster defines a thruster interface.
type Thruster interface {
	// Returns the minimum power and voltage requirements for this thruster.
	Min() (voltage, power uint)
	// Returns the max power and voltage requirements for this thruster.
	Max() (voltage, power uint)
	// Returns the thrust in Newtons and isp consumed in seconds.
	Thrust(voltage, power uint) (thrust, isp float64)
}

/* Available thrusters */

// PPS1350 is the Snecma thruster used on SMART-1.
// Source: http://www.esa.int/esapub/bulletin/bulletin129/bul129e_estublier.pdf
type PPS1350 struct{}

// Min implements the Thruster interface.
func (t *PPS1350) Min() (voltage, power uint) {
	return t.Max()
}

// Max implements the Thruster interface.
func (t *PPS1350) Max() (voltage, power uint) {
	return 350, 2500
}

// Thrust implements the Thruster interface.
func (t *PPS1350) Thrust(voltage, power uint) (thrust, fuelMass float64) {
	if voltage == 350 && power == 2500 {
		return 140 * 1e-3, 1800
	}
	panic("unsupported voltage or power provided")
}

// HPHET12k5 is based on the NASA & Rocketdyne 12.5kW demo
/*type HPHET12k5 struct{}

// Min implements the Thruster interface.
func (t *HPHET12k5) Min() (voltage, power uint) {
	return 400, 12500
}

// Max implements the Thruster interface.
func (t *HPHET12k5) Max() (voltage, power uint) {
	return 400, 12500
}

// Thrust implements the Thruster interface.
func (t *HPHET12k5) Thrust(voltage, power uint) (thrust, fuelMass float64) {
	if voltage == 400 && power == 12500 {
		return 0.680, 4.8 * 1e-5 // fuel usage made up assuming linear from power.
	}
	panic("unsupported voltage or power provided")
}*/

// GenericEP is a generic EP thruster.
type GenericEP struct {
	thrust float64
	isp    float64
}

// Min implements the Thruster interface.
func (t *GenericEP) Min() (voltage, power uint) {
	return 0, 0
}

// Max implements the Thruster interface.
func (t *GenericEP) Max() (voltage, power uint) {
	return 0, 0
}

// Thrust implements the Thruster interface.
func (t *GenericEP) Thrust(voltage, power uint) (thrust, isp float64) {
	return t.thrust, t.isp
}

// NewGenericEP returns a generic electric prop thruster.
func NewGenericEP(thrust, isp float64) *GenericEP {
	return &GenericEP{thrust, isp}
}

/* Let's define some control laws. */

// Coast defines an thrust control law which does not thrust.
type Coast struct {
	reason string
}

// Reason implements the ThrustControl interface.
func (cl Coast) Reason() string {
	return cl.reason
}

// Type implements the ThrustControl interface.
func (cl Coast) Type() ControlLaw {
	return coast
}

// Control implements the ThrustControl interface.
func (cl Coast) Control(o Orbit) []float64 {
	return []float64{0, 0, 0}
}

// Tangential defines a tangential thrust control law
type Tangential struct {
	reason string
}

// Reason implements the ThrustControl interface.
func (cl Tangential) Reason() string {
	return cl.reason
}

// Type implements the ThrustControl interface.
func (cl Tangential) Type() ControlLaw {
	return tangential
}

// Control implements the ThrustControl interface.
func (cl Tangential) Control(o Orbit) []float64 {
	return unit(o.V)
}

// AntiTangential defines an antitangential thrust control law
type AntiTangential struct {
	reason string
}

// Reason implements the ThrustControl interface.
func (cl AntiTangential) Reason() string {
	return cl.reason
}

// Type implements the ThrustControl interface.
func (cl AntiTangential) Type() ControlLaw {
	return antiTangential
}

// Control implements the ThrustControl interface.
func (cl AntiTangential) Control(o Orbit) []float64 {
	unitV := unit(o.V)
	unitV[0] *= -1
	unitV[1] *= -1
	unitV[2] *= -1
	return unitV
}

// Inversion keeps the thrust as tangential but inverts its direction within an angle from the orbit apogee.
// This leads to collisions with main body if the orbit isn't circular enough.
// cf. Izzo et al. (https://arxiv.org/pdf/1602.00849v2.pdf)
type Inversion struct {
	ν      float64
	reason string
}

// Reason implements the ThrustControl interface.
func (cl Inversion) Reason() string {
	return cl.reason
}

// Type implements the ThrustControl interface.
func (cl Inversion) Type() ControlLaw {
	return inversion
}

// Control implements the ThrustControl interface.
func (cl Inversion) Control(o Orbit) []float64 {
	f := o.Getν()
	if _, e := o.GetE(); e > 0.01 || (f > cl.ν-math.Pi && f < math.Pi-cl.ν) {
		return Tangential{}.Control(o)
	}
	return AntiTangential{}.Control(o)
}

/* Following optimal thrust change are from IEPC 2011's paper:
Low-Thrust Maneuvers for the Efficient Correction of Orbital Elements
A. Ruggiero, S. Marcuccio and M. Andrenucci */

func unitΔvFromAngles(α, β float64) []float64 {
	sinα, cosα := math.Sincos(α)
	sinβ, cosβ := math.Sincos(β)
	return []float64{cosβ * sinα, cosα * cosβ, sinβ}
}

// OptimalThrust is an optimal thrust.
type OptimalThrust struct {
	ctrl func(o Orbit) []float64
	GenericCL
}

// Control implements the ThrustControl interface.
func (cl OptimalThrust) Control(o Orbit) []float64 {
	return cl.ctrl(o)
}

// NewOptimalThrust returns a new optimal Δe.
func NewOptimalThrust(cl ControlLaw, reason string) ThrustControl {
	var ctrl func(o Orbit) []float64
	switch cl {
	case optiΔa:
		ctrl = func(o Orbit) []float64 {
			_, e := o.GetE()
			sinν, cosν := math.Sincos(o.Getν())
			return unitΔvFromAngles(math.Atan(e*sinν/(1+e*cosν)), 0.0)
		}
		break
	case optiΔe:
		ctrl = func(o Orbit) []float64 {
			_, cosE := o.GetSinCosE()
			sinν, cosν := math.Sincos(o.Getν())
			return unitΔvFromAngles(math.Atan(sinν/(cosE+cosν)), 0.0)
		}
		break
	case optiΔi:
		ctrl = func(o Orbit) []float64 {
			return unitΔvFromAngles(0.0, sign(math.Cos(o.Getω()+o.Getν()))*math.Pi/2)
		}
		break
	case optiΔΩ:
		ctrl = func(o Orbit) []float64 {
			return unitΔvFromAngles(0.0, sign(math.Sin(o.Getω()+o.Getν()))*math.Pi/2)
		}
		break
	case optiΔω:
		ctrl = func(o Orbit) []float64 {
			_, e := o.GetE()
			ν := o.Getν()
			cotν := 1 / math.Tan(ν)
			coti := 1 / math.Tan(o.GetI())
			sinν, cosν := math.Sincos(o.Getν())
			sinων := math.Sin(o.Getω() + ν)
			α := math.Atan((1 + e*cosν) * cotν / (2 + e*cosν))
			sinαν := math.Sin(α - ν)
			β := math.Atan((e * coti * sinων) / (sinαν*(1+e*cosν) - math.Cos(α)*sinν))
			return unitΔvFromAngles(α, β)
		}
		break
	default:
		panic(fmt.Errorf("optmized %s not yet implemented", cl))
	}
	return OptimalThrust{ctrl, GenericCL{reason, cl}}
}

// OptimalΔOrbit combines all the control laws from Ruggiero et al.
type OptimalΔOrbit struct {
	Initd          bool
	ainit, atarget float64
	iinit, itarget float64
	einit, etarget float64
	Ωinit, Ωtarget float64
	ωinit, ωtarget float64
	controls       []ThrustControl
	GenericCL
}

// NewOptimalΔOrbit generates a new OptimalΔOrbit based on the provided target orbit.
func NewOptimalΔOrbit(target Orbit, laws ...ThrustControl) *OptimalΔOrbit {
	cl := OptimalΔOrbit{}
	cl.atarget, cl.etarget, cl.itarget, cl.ωtarget, cl.Ωtarget, _ = target.OrbitalElements()
	cl.controls = laws
	cl.GenericCL = GenericCL{"ΔOrbit", multiOpti}
	return &cl
}

// Control implements the ThrustControl interface.
func (cl *OptimalΔOrbit) Control(o Orbit) []float64 {
	thrust := []float64{0, 0, 0}
	if !cl.Initd {
		cl.ainit, cl.einit, cl.iinit, cl.ωinit, cl.Ωinit, _ = o.OrbitalElements()
		cl.Initd = true
		return thrust
	}

	factor := func(oscul, init, target float64) float64 {
		if math.Abs(init-target) < 1e-8 {
			return 0
		}
		return (target - oscul) / (target - init)
	}

	for _, ctrl := range cl.controls {
		var oscul, init, target float64
		switch ctrl.Type() {
		case optiΔa:
			oscul = o.GetA()
			init = cl.ainit
			target = cl.atarget
		case optiΔe:
			_, oscul = o.GetE()
			init = cl.einit
			target = cl.etarget
		case optiΔi:
			oscul = o.GetI()
			init = cl.iinit
			target = cl.itarget
		case optiΔΩ:
			oscul = o.GetΩ()
			init = cl.Ωinit
			target = cl.Ωtarget
		case optiΔω:
			oscul = o.Getω()
			init = cl.ωinit
			target = cl.ωtarget
		}
		fact := factor(oscul, init, target)
		tmpThrust := ctrl.Control(o)
		for i := 0; i < 3; i++ {
			thrust[i] += fact * tmpThrust[i]
		}
	}
	return unit(thrust)
}
