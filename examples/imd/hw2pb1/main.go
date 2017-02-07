package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/ChristopherRabotin/smd"
	"github.com/ChristopherRabotin/smd/tools"
	"github.com/gonum/matrix/mat64"
)

func main() {
	launchDT := time.Date(2018, 5, 1, 0, 0, 0, 0, time.UTC)
	// Let's set an initial estimated arrival time, it won't take less than 3 months.
	arrivalDT := time.Date(2018, 8, 1, 0, 0, 0, 0, time.UTC)
	// RV() is a pointer method (because of the cache update)
	earthOrbit := smd.Earth.HelioOrbit(launchDT)
	REarthF, VEarthF := earthOrbit.RV()
	fmt.Printf("R=%+v\nV=%+v\n", REarthF, VEarthF)
	Rearth := mat64.NewVector(3, REarthF)
	nVearth := mat64.Norm(mat64.NewVector(3, VEarthF), 2)
	for _, dm := range []struct {
		Type rune
		val  float64
	}{{1, 1}, {2, -1}} {
		// Initialize the CSV string
		csvContent := fmt.Sprintf("# Earth -> Mars Lambert type %d\n#Launch: %s\n#Initial arrival:%s\ndays,c3,vInf\n", dm.Type, launchDT, arrivalDT)
		minC3 := 10e4
		minVinf := 10e4
		minDurC3 := 0
		minDurVinf := 0
		for days := 0; days < 250; days++ {
			duration := arrivalDT.Add(time.Duration(days) * 24 * time.Hour).Sub(launchDT)
			Rmars := mat64.NewVector(3, smd.Mars.HelioOrbit(launchDT).R())
			Vi, _, _, err := tools.Lambert(Rearth, Rmars, duration, dm.val, smd.Sun)
			if err != nil {
				fmt.Printf("[ERROR] %s: %s\n", duration, err)
				continue
			}
			// Compute the v_infinity
			vInf := mat64.Norm(Vi, 2) - nVearth
			c3 := math.Pow(vInf, 2)
			// Add to CSV
			csvContent += fmt.Sprintf("%d,%f,%f\n", days, c3, vInf)
			// Check if min
			if vInf < minVinf {
				minVinf = vInf
				minDurVinf = days
			}
			if c3 < minC3 {
				minC3 = c3
				minDurC3 = days
			}
		}
		fmt.Printf("===== MIN Type %d ======\nvInf=%f\tdur=%d\nc3=%f\tdur=%d\n=======================\n", dm.Type, minVinf, minDurVinf, minC3, minDurC3)
		// Write CSV file.
		f, err := os.Create(fmt.Sprintf("./pb1-type-%d.csv", dm.Type))
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if _, err := f.WriteString(csvContent); err != nil {
			panic(err)
		}
	}
}
