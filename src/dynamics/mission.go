package dynamics

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ChristopherRabotin/ode"
)

const (
	stepSize = 10
	stepUnit = time.Second
)

var wg sync.WaitGroup

/* Handles the astrodynamical propagations. */

// Mission defines a mission and does the propagation.
type Mission struct {
	Vehicle        *Spacecraft // As pointer because SC may be altered during propagation.
	Orbit          *Orbit      // As pointer because the orbit changes during propagation.
	StartDT        time.Time
	EndDT          time.Time
	CurrentDT      time.Time
	includeJ2      bool
	stopChan       chan (bool)
	histChan       chan<- (AstroState)
	done, collided bool
}

// NewMission returns a new Mission instance from the position and velocity vectors.
func NewMission(s *Spacecraft, o *Orbit, start, end time.Time, includeJ2 bool, conf ExportConfig) *Mission {
	// If no filepath is provided, then no output will be written.
	var histChan chan (AstroState)
	if !conf.IsUseless() {
		histChan = make(chan (AstroState), 1000) // a 1k entry buffer
		wg.Add(1)
		go func() {
			defer wg.Done()
			StreamStates(conf, histChan)
		}()
	} else {
		histChan = nil
	}
	// Must switch to UTC as all ephemeris data is in UTC.
	if start.Location() != time.UTC {
		start = start.UTC()
	}
	if end.Location() != time.UTC {
		end = end.UTC()
	}

	a := &Mission{s, o, start, end, start, includeJ2, make(chan (bool), 1), histChan, false, false}
	// Write the first data point.
	if histChan != nil {
		histChan <- AstroState{a.CurrentDT, *s, *o}
	}

	if end.Before(start) {
		a.Vehicle.logger.Log("level", "warning", "subsys", "astro", "message", "no end date")
	}

	return a
}

// LogStatus returns the status of the propagation and vehicle.
func (a *Mission) LogStatus() {
	a.Vehicle.logger.Log("level", "info", "subsys", "astro", "date", a.CurrentDT, "fuel(kg)", a.Vehicle.FuelMass, "orbit", a.Orbit)
}

// Propagate starts the propagation.
func (a *Mission) Propagate() {
	// Add a ticker status report based on the duration of the simulation.
	a.LogStatus()
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for _ = range ticker.C {
			if a.done {
				break
			}
			a.LogStatus()
		}
	}()
	vInit := norm(a.Orbit.GetV())
	// XXX: The step size may very well be wrong here!!!
	// In fact, it does not account for the unit *and* changing to a much smaller stepsize massively
	// breaks stuff (e.g. a one second step size leads to a higher true anomaly for the GEO half orbit
	// as a ten second step, but a 1 Microsecond leads to a different error).
	// With a 1 Millisecond time step, several orbits are done in just a few hours...
	// spacecraft=test level=info subsys=astro date=2017-01-17T02:32:18.434321007Z (...) ν=0.000000
	// spacecraft=test level=info subsys=astro date=2017-01-17T04:06:29.330321007Z (...) ν=214.618862
	// spacecraft=test level=info subsys=astro date=2017-01-17T05:45:21.279321007Z (...) ν=163.725517
	// spacecraft=test level=info subsys=astro date=2017-01-17T07:19:20.241321007Z (...) ν=328.460602
	// spacecraft=test level=info subsys=astro date=2017-01-17T08:56:50.884321007Z (...) ν=297.792346
	// spacecraft=test level=info subsys=astro date=2017-01-17T10:32:42.367321007Z (...) ν=212.738839
	ode.NewRK4(0, stepSize, a).Solve() // Blocking.
	vFinal := norm(a.Orbit.GetV())
	a.done = true
	duration := a.CurrentDT.Sub(a.StartDT)
	durStr := duration.String()
	if duration.Hours() > 24 {
		durStr += fmt.Sprintf(" (~%.1fd)", duration.Hours()/24)
	}
	a.Vehicle.logger.Log("level", "notice", "subsys", "astro", "status", "finished", "duration", durStr, "Δv(km/s)", math.Abs(vFinal-vInit))
	a.LogStatus()
	if a.Vehicle.FuelMass < 0 {
		a.Vehicle.logger.Log("level", "critical", "subsys", "prop", "fuel(kg)", a.Vehicle.FuelMass)
	}
	wg.Wait() // Don't return until we're done writing all the files.
}

// StopPropagation is used to stop the propagation before it is completed.
func (a *Mission) StopPropagation() {
	a.stopChan <- true
}

// Stop implements the stop call of the integrator. To stop the propagation, call StopPropagation().
func (a *Mission) Stop(i uint64) bool {
	select {
	case <-a.stopChan:
		if a.histChan != nil {
			close(a.histChan)
		}
		return true // Stop because there is a request to stop.
	default:
		a.CurrentDT = a.CurrentDT.Add(time.Duration(stepSize) * stepUnit)
		if a.EndDT.Before(a.StartDT) {
			// Check if any waypoint still needs to be reached.
			for _, wp := range a.Vehicle.WayPoints {
				if !wp.Cleared() {
					return false
				}
			}
			if a.histChan != nil {
				close(a.histChan)
			}
			return true
		}
		if a.CurrentDT.Sub(a.EndDT).Nanoseconds() > 0 {
			if a.histChan != nil {
				close(a.histChan)
			}
			return true // Stop, we've reached the end of the simulation.
		}
	}
	return false
}

// GetState returns the state for the integrator for the Gaussian VOP.
func (a *Mission) GetState() (s []float64) {
	s = make([]float64, 7)
	s[0] = a.Orbit.a
	s[1] = a.Orbit.e
	s[2] = a.Orbit.i
	s[3] = a.Orbit.Ω
	s[4] = a.Orbit.ω
	s[5] = a.Orbit.ν
	s[6] = a.Vehicle.FuelMass
	return
}

// SetState sets the updated state.
func (a *Mission) SetState(i uint64, s []float64) {
	if a.histChan != nil {
		a.histChan <- AstroState{a.CurrentDT, *a.Vehicle, *a.Orbit}
	}
	// Note that we modulo here *and* in Func because the last step of the integrator
	// adds up all the previous values with weights!
	a.Orbit.a = s[0]
	a.Orbit.e = math.Abs(s[1]) // eccentricity is always a positive number
	a.Orbit.i = math.Mod(s[2], 2*math.Pi)
	a.Orbit.Ω = math.Mod(s[3], 2*math.Pi)
	a.Orbit.ω = math.Mod(s[4], 2*math.Pi)
	a.Orbit.ν = math.Mod(s[5], 2*math.Pi)

	// Let's execute any function which is in the queue of this time step.
	for _, f := range a.Vehicle.FuncQ {
		if f == nil {
			continue
		}
		f()
	}
	a.Vehicle.FuncQ = make([]func(), 5) // Clear the queue.

	// Orbit sanity checks and warnings.
	if !a.collided && a.Orbit.GetRNorm() < a.Orbit.Origin.Radius {
		a.collided = true
		a.Vehicle.logger.Log("level", "critical", "subsys", "astro", "collided", a.Orbit.Origin.Name, "dt", a.CurrentDT)
	} else if a.collided && a.Orbit.GetRNorm() > a.Orbit.Origin.Radius*1.01 {
		// Now further from the 1% dead zone
		a.collided = false
		a.Vehicle.logger.Log("level", "critical", "subsys", "astro", "revived", a.Orbit.Origin.Name, "dt", a.CurrentDT)
	} else if a.Orbit.GetRNorm() > a.Orbit.Origin.SOI {
		a.Vehicle.ToXCentric(Sun, a.CurrentDT, a.Orbit)
	}

	// Propulsion sanity check
	if a.Vehicle.FuelMass > 0 && s[6] <= 0 {
		a.Vehicle.logger.Log("level", "critical", "subsys", "prop", "fuel(kg)", s[6])
	}
	a.Vehicle.FuelMass = s[6]
}

// Func is the integration function using Gaussian VOP as per Ruggiero et al. 2011.
func (a *Mission) Func(t float64, f []float64) (fDot []float64) {
	// Fix the angles in case the sum in integrator lead to an overflow.
	for i := 2; i < 6; i++ {
		f[i] = math.Mod(f[i], 2*math.Pi)
	}
	tmpOrbit := NewOrbitFromOE(f[0], f[1], f[2], f[3], f[4], f[5], a.Orbit.Origin)
	p := tmpOrbit.GetSemiParameter()
	h := tmpOrbit.GetHNorm()
	r := tmpOrbit.GetRNorm()
	sini, cosi := math.Sincos(tmpOrbit.i)
	sinν, cosν := math.Sincos(tmpOrbit.ν)
	sinζ, cosζ := math.Sincos(tmpOrbit.ω + tmpOrbit.ν)
	fDot = make([]float64, 7) // init return vector
	// Let's add the thrust to increase the magnitude of the velocity.
	Δv, usedFuel := a.Vehicle.Accelerate(a.CurrentDT, a.Orbit)
	fR := Δv[0]
	fS := Δv[1]
	fW := Δv[2]
	// da/dt
	fDot[0] = ((2 * tmpOrbit.a * tmpOrbit.a) / h) * (tmpOrbit.e*sinν*fR + (p/r)*fS)
	// de/dt
	fDot[1] = (p*sinν*fR + fS*((p+r)*cosν+r*tmpOrbit.e)) / h
	// di/dt
	fDot[2] = fW * r * cosζ / h
	// dΩ/dt
	fDot[3] = fW * r * sinζ / (h * sini)
	// dω/dt
	fDot[4] = (-p*cosν*fR+(p+r)*sinν*fS)/(h*tmpOrbit.e) - fDot[3]*cosi
	// dν/dt -- as per Vallado, page 636 (with errata of 4th edition.)
	fDot[5] = h/(r*r) + ((p*cosν*fR)-(p+r)*sinν*fS)/(tmpOrbit.e*h)
	// d(fuel)/dt
	fDot[6] = -usedFuel
	if a.includeJ2 && tmpOrbit.Origin.J2 > 0 {
		// d\bar{Ω}/dt
		fDot[3] += -(3 * math.Sqrt(tmpOrbit.Origin.μ/math.Pow(tmpOrbit.a, 3)) * tmpOrbit.Origin.J2 / 2) * math.Pow(tmpOrbit.Origin.Radius/p, 2) * cosi
		// d\bar{ω}/dt
		fDot[4] += -(3 * math.Sqrt(tmpOrbit.Origin.μ/math.Pow(tmpOrbit.a, 3)) * tmpOrbit.Origin.J2 / 4) * math.Pow(tmpOrbit.Origin.Radius/p, 2) * (5*math.Pow(cosi, 2) - 1)
		// TODO: add effect on true anomaly.
	}
	for i := 0; i < 7; i++ {
		if i > 2 && i < 6 {
			fDot[i] = math.Mod(fDot[i], 2*math.Pi)
		}
		if math.IsNaN(fDot[i]) {
			Rcur, Vcur := a.Orbit.GetRV()
			Rtmp, Vtmp := tmpOrbit.GetRV()
			panic(fmt.Errorf("fDot[%d]=NaN @ dt=%s\np=%f\th=%f\tdv=%+v\ntmp:%s\ncur:%s\nR_cur=%+v\tV_cur=%+v\nR_tmp=%+v\tV_tmp=%+v", i, a.CurrentDT, p, h, Δv, tmpOrbit, a.Orbit, Rcur, Vcur, Rtmp, Vtmp))
		}
	}
	return
}

// AstroState stores propagated state.
type AstroState struct {
	dt    time.Time
	sc    Spacecraft
	orbit Orbit
}