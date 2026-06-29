// Package shadow calculates sun/shade conditions for a geographic location
// throughout a day, using DOM (Digital Surface Model) elevation tiles to
// determine whether buildings or terrain block the direct line to the sun.
//
// Algorithm overview:
//
//  1. Convert the query lat/lon to UTM.
//  2. Look up the DTM ground height at that point; add the observer eye height.
//  3. For each minute between sunrise and sunset, get the sun's azimuth and altitude.
//  4. Cast a ray from the observer toward the sun.  At each step, compare the
//     ray height against the DOM surface.  If the surface is higher → blocked.
package shadow

import (
	"fmt"
	"math"
	"time"

	"github.com/optimuscrime/sunspot-core/internal/sun"
	"github.com/optimuscrime/sunspot-core/internal/utm"
)

// Options tunes the shadow calculation.  The zero value is not useful —
// use DefaultOptions() to get sensible defaults.
type Options struct {
	// ObserverHeight is how many metres above the DTM ground level the
	// observer's eyes are.  1.5 m is a typical standing eye height.
	ObserverHeight float64

	// MinSunAltitude is the sun elevation angle (degrees) below which we skip
	// ray casting and always report shade.  Very low angles require checking
	// kilometres of terrain which is unreliable and practically irrelevant.
	MinSunAltitude float64

	// MaxObstacleHeight is the assumed maximum height (metres) of any obstacle.
	// It caps the ray trace distance as: maxDist = MaxObstacleHeight / tan(altitude).
	MaxObstacleHeight float64
}

// DefaultOptions returns the options used for typical Oslo urban analysis.
func DefaultOptions() Options {
	return Options{
		ObserverHeight:    1.5,
		MinSunAltitude:    5.0,
		MaxObstacleHeight: 200.0,
	}
}

// BlockPoint is where a sun ray first hit an obstacle.
// Coordinates are in WGS84 (latitude/longitude).
type BlockPoint struct {
	Lat           float64 // WGS84 latitude of the blocking pixel
	Lon           float64 // WGS84 longitude of the blocking pixel
	Distance      float64 // horizontal metres from the observer
	SurfaceHeight float32 // DOM elevation of the blocking pixel in metres
}

// Condition is the sun/shade state at one specific minute.
type Condition struct {
	Time      time.Time
	Sun       sun.Position
	InSun     bool
	BlockedBy *BlockPoint // non-nil when shade and sun is above the minimum altitude
}

// DayConditions returns a Condition for every minute between sunrise and sunset
// on the day that contains date.
//
// domIndex is used to detect blocking obstacles along the sun ray.
// dtmIndex is used to determine the observer's ground elevation.  If nil,
// the DOM height is used instead (appropriate for a rooftop observer).
func DayConditions(date time.Time, latDeg, lonDeg float64, domIndex, dtmIndex *TileIndex, opts Options) ([]Condition, error) {
	daylight := sun.GetDaylight(date, latDeg, lonDeg)

	easting, northing, zone, northern := utm.FromLatLon(latDeg, lonDeg, 0)

	groundIndex := domIndex
	if dtmIndex != nil {
		groundIndex = dtmIndex
	}
	groundHeight, ok := groundIndex.ElevationAt(easting, northing)
	if !ok {
		return nil, fmt.Errorf("%w: no elevation tile covers (%.5f°, %.5f°)",
			ErrOutOfCoverage, latDeg, lonDeg)
	}

	observerElevation := float64(groundHeight) + opts.ObserverHeight

	totalMinutes := int(daylight.Sunset.Sub(daylight.Sunrise)/time.Minute) + 1
	conditions := make([]Condition, 0, totalMinutes)

	for t := daylight.Sunrise; !t.After(daylight.Sunset); t = t.Add(time.Minute) {
		pos := sun.At(t, latDeg, lonDeg)

		var inSun bool
		var blockedBy *BlockPoint

		if pos.Altitude < opts.MinSunAltitude {
			inSun = false
		} else {
			blockedBy = castRay(domIndex, easting, northing, observerElevation, zone, northern, pos, opts)
			inSun = blockedBy == nil
		}

		conditions = append(conditions, Condition{
			Time:      t,
			Sun:       pos,
			InSun:     inSun,
			BlockedBy: blockedBy,
		})
	}

	return mergeShortShade(conditions), nil
}

// mergeShortShade reclassifies consecutive shade runs shorter than
// MinShadeDuration as sun, so transient dips don't pollute the output.
func mergeShortShade(conditions []Condition) []Condition {
	i := 0
	for i < len(conditions) {
		if conditions[i].InSun {
			i++
			continue
		}
		j := i
		for j < len(conditions) && !conditions[j].InSun {
			j++
		}
		if time.Duration(j-i)*time.Minute < MinShadeDuration {
			for k := i; k < j; k++ {
				conditions[k].InSun = true
				conditions[k].BlockedBy = nil
			}
		}
		i = j
	}
	return conditions
}

// castRay marches from (e, n, height) toward the sun and returns the first
// point where the DOM surface rises above the ray, or nil for clear sky.
//
// Step size starts at 1 m and grows proportionally with distance (see
// stepGrowth), so nearby obstacles are detected at full 1 m precision while
// distant checks become coarser as the potential impact diminishes.
func castRay(
	idx *TileIndex,
	e, n, height float64,
	zone int, northern bool,
	pos sun.Position,
	opts Options,
) *BlockPoint {
	if pos.Altitude < opts.MinSunAltitude {
		return nil
	}

	azRad := pos.Azimuth * math.Pi / 180.0
	altRad := pos.Altitude * math.Pi / 180.0
	tanAlt := math.Tan(altRad)

	// Unit vector toward the sun along the ground plane.
	dE := math.Sin(azRad)
	dN := math.Cos(azRad)

	// Stop once the ray is guaranteed above all plausible obstacles.
	maxDist := math.Min(opts.MaxObstacleHeight/tanAlt, 5000.0)

	for d := 1.0; d <= maxDist; d += math.Max(1.0, d*stepGrowth) {
		checkE := e + d*dE
		checkN := n + d*dN
		rayHeight := height + d*tanAlt

		surfHeight, ok := idx.ElevationAt(checkE, checkN)
		if !ok {
			return nil // left the surveyed area — treat as clear sky
		}

		if float64(surfHeight) > rayHeight {
			lat, lon := utm.ToLatLon(checkE, checkN, zone, northern)
			return &BlockPoint{
				Lat:           lat,
				Lon:           lon,
				Distance:      d,
				SurfaceHeight: surfHeight,
			}
		}
	}

	return nil
}

// stepGrowth is the per-step fractional increase in ray-march step size.
// The step at distance d is max(1 m, d × stepGrowth), giving 1 m resolution
// up to 50 m and coarser sampling beyond (e.g. ~10 m at 500 m).
const stepGrowth = 0.02

// MinShadeDuration is the shortest shade period that is reported as actual shade.
// Consecutive shade runs shorter than this are reclassified as sun, since they
// are likely noise from ray-casting at grazing angles or narrow obstacles.
const MinShadeDuration = 10 * time.Minute

// ErrOutOfCoverage is returned when the query coordinate falls outside all
// loaded tiles.
var ErrOutOfCoverage = fmt.Errorf("point outside tile coverage")
