// Package sun calculates the position of the sun in the sky for a given
// geographic coordinate and point in time.
//
// It wraps github.com/sixdouglas/suncalc (a Go port of the widely-used
// SunCalc.js by Vladimir Agafonkin) and re-expresses its output in the
// conventions that are most natural for shadow and illumination analysis:
//
//   - Azimuth in degrees, measured clockwise from North (0° = N, 90° = E,
//     180° = S, 270° = W).  The underlying library uses radians measured
//     from South towards West; we convert.
//   - Altitude (elevation angle) in degrees above the horizon.  Negative
//     values mean the sun is below the horizon.
//
// Geographic coordinates must be in WGS84 decimal degrees.
// Projected coordinates (e.g. UTM) must be converted before calling.
package sun

import (
	"math"
	"time"

	"github.com/sixdouglas/suncalc"
)

// Position holds the sun's location in the sky.
type Position struct {
	// Azimuth is the compass bearing of the sun in degrees, measured
	// clockwise from North.  Range: [0, 360).
	Azimuth float64

	// Altitude is the angle of the sun above the horizon in degrees.
	// Positive means above the horizon; negative means below.
	Altitude float64
}

func (p Position) IsAboveHorizon() bool {
	return p.Altitude > 0
}

// Daylight holds the key solar times for a single calendar day.
type Daylight struct {
	Sunrise time.Time
	Sunset  time.Time
	Noon    time.Time
}

// GetDaylight returns the sunrise, solar noon, and sunset times for a WGS84
// coordinate on the day that contains t.  The returned times are in the same
// timezone as t.
func GetDaylight(t time.Time, latDeg, lonDeg float64) Daylight {
	times := suncalc.GetTimes(t, latDeg, lonDeg)
	return Daylight{
		Sunrise: times[suncalc.Sunrise].Value.In(t.Location()),
		Sunset:  times[suncalc.Sunset].Value.In(t.Location()),
		Noon:    times[suncalc.SolarNoon].Value.In(t.Location()),
	}
}

// At returns the sun's position for a WGS84 coordinate (lat, lon in decimal
// degrees) at the given moment in time.
//
// The time should carry a timezone so that solar noon is computed correctly.
// Passing UTC is fine — the algorithm works in UTC internally.
func At(t time.Time, latDeg, lonDeg float64) Position {
	raw := suncalc.GetPosition(t, latDeg, lonDeg)

	// suncalc returns azimuth in radians, measured from South, positive
	// towards West (counter-clockwise when viewed from above):
	//   0  = South,  +π/2 = West,  ±π = North,  −π/2 = East
	//
	// Convert to the standard compass convention: degrees clockwise from North.
	//   Add π to shift the zero from South to North, then convert to degrees.
	//   (No sign flip needed — adding π already maps the counter-clockwise
	//   South-origin convention to the clockwise North-origin convention.)
	azimuthDeg := math.Mod((raw.Azimuth+math.Pi)*180.0/math.Pi+360.0, 360.0)
	altitudeDeg := raw.Altitude * 180.0 / math.Pi

	return Position{
		Azimuth:  azimuthDeg,
		Altitude: altitudeDeg,
	}
}
