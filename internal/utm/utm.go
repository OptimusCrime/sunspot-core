// Package utm converts between UTM projected coordinates and geographic
// latitude/longitude (WGS84 / ETRS89).
//
// Both directions are implemented using Redfearn's series for the Transverse
// Mercator projection, as specified in DMA Technical Manual TM 8358.2
// ("The Universal Grids").  Accuracy is better than 1 mm within a UTM zone.
//
// Our elevation tiles use EPSG:25832 (ETRS89 / UTM Zone 32N).  The ETRS89
// datum uses the GRS80 ellipsoid, which is numerically identical to WGS84
// for all practical purposes (difference < 0.1 mm).  The returned
// latitude/longitude values can therefore be passed directly to WGS84-based
// functions such as sun position calculation.
package utm

import "math"

// GRS80 / WGS84 ellipsoid parameters.
const (
	a  = 6_378_137.0         // semi-major axis, metres
	f  = 1.0 / 298.257222101 // flattening (GRS80; WGS84 differs in the 10th decimal)
	k0 = 0.9996              // UTM scale factor on the central meridian
)

// Derived ellipsoid constants.
var (
	b   = a * (1 - f)       // semi-minor axis
	e2  = 2*f - f*f         // first eccentricity squared
	ep2 = e2 / (1 - e2)     // second eccentricity squared (e'²)
	n   = (a - b) / (a + b) // third flattening
)

// ToLatLon converts UTM easting/northing to geographic coordinates.
//
//   - easting and northing are in metres
//   - zone is the UTM zone number (1–60); for our Oslo data this is 32
//   - northern must be true for the northern hemisphere
//
// Returns latitude and longitude in decimal degrees (WGS84 / ETRS89).
func ToLatLon(easting, northing float64, zone int, northern bool) (latDeg, lonDeg float64) {
	// Central meridian of the zone in radians.
	lambda0 := deg2rad(float64(zone*6 - 183))

	// Remove UTM false offsets.
	// False easting is always 500 000 m; false northing is 0 in the northern
	// hemisphere and 10 000 000 m in the southern hemisphere.
	x := easting - 500_000.0
	y := northing
	if !northern {
		y -= 10_000_000.0
	}

	// -----------------------------------------------------------------------
	// Step 1: recover the footpoint latitude φ₁ from the meridian arc.
	//
	// The UTM northing N = k₀ · M(φ), so M = N/k₀.
	// We then invert the meridian arc series to find the "footpoint latitude"
	// using the Helmert series in terms of the third flattening n.
	// -----------------------------------------------------------------------
	n2 := n * n
	n3 := n2 * n
	n4 := n2 * n2

	// Rectifying radius (mean meridional radius).
	A := a / (1 + n) * (1 + n2/4 + n4/64)

	// Rectifying latitude µ from the meridian arc M = y/k₀.
	mu := (y / k0) / A

	// Inverse series: φ₁ = µ + Σ αᵢ·sin(2i·µ)  (Helmert 1880)
	phi1 := mu +
		(3.0/2*n-27.0/32*n3)*math.Sin(2*mu) +
		(21.0/16*n2-55.0/32*n4)*math.Sin(4*mu) +
		(151.0/96*n3)*math.Sin(6*mu) +
		(1097.0/512*n4)*math.Sin(8*mu)

	// -----------------------------------------------------------------------
	// Step 2: apply the Redfearn series to get latitude and longitude.
	// (DMA TM 8358.2, equations 2-10 and 2-11)
	// -----------------------------------------------------------------------
	sinPhi1 := math.Sin(phi1)
	cosPhi1 := math.Cos(phi1)
	t1 := math.Tan(phi1) // T₁ = tan φ₁
	t1sq := t1 * t1      // T₁²

	// Radii of curvature at φ₁.
	nu1 := a / math.Sqrt(1-e2*sinPhi1*sinPhi1)              // prime vertical
	rho1 := a * (1 - e2) / math.Pow(1-e2*sinPhi1*sinPhi1, 1.5) // meridian

	c1 := ep2 * cosPhi1 * cosPhi1 // C₁ = e'²·cos²φ₁

	// D is the scaled easting offset (DMA TM 8358.2 eq. 2-9).
	// Note: x = E − 500 000 (raw offset, not yet scaled by k₀).
	D := x / (nu1 * k0)
	D2 := D * D
	D3 := D2 * D
	D4 := D2 * D2
	D5 := D4 * D
	D6 := D4 * D2

	// Latitude (DMA TM 8358.2 eq. 2-10).
	lat := phi1 - (nu1*t1/rho1)*(
		D2/2-
			D4/24*(5+3*t1sq+10*c1-4*c1*c1-9*ep2)+
			D6/720*(61+90*t1sq+298*c1+45*t1sq*t1sq-252*ep2-3*c1*c1))

	// Longitude (DMA TM 8358.2 eq. 2-11).
	lon := lambda0 + (1/cosPhi1)*(
		D-
			D3/6*(1+2*t1sq+c1)+
			D5/120*(5-2*c1+28*t1sq-3*c1*c1+8*ep2+24*t1sq*t1sq))

	return rad2deg(lat), rad2deg(lon)
}

// FromLatLon converts geographic coordinates to UTM easting/northing.
//
//   - latDeg and lonDeg are in decimal degrees (WGS84 / ETRS89)
//   - zone is the UTM zone number; pass 0 to auto-select the correct zone
//
// Returns easting and northing in metres, the zone used, and whether the
// point is in the northern hemisphere.
func FromLatLon(latDeg, lonDeg float64, zone int) (easting, northing float64, usedZone int, northern bool) {
	if zone == 0 {
		zone = int((lonDeg+180)/6) + 1
	}
	northern = latDeg >= 0

	lambda0 := deg2rad(float64(zone*6 - 183))
	phi := deg2rad(latDeg)
	lambda := deg2rad(lonDeg)

	sinPhi := math.Sin(phi)
	cosPhi := math.Cos(phi)
	t := math.Tan(phi)
	t2 := t * t
	c := ep2 * cosPhi * cosPhi // C = e'²·cos²φ
	c2 := c * c

	// Prime vertical radius.
	nu := a / math.Sqrt(1-e2*sinPhi*sinPhi)

	// Meridian arc from the equator to φ.
	n2 := n * n
	n3 := n2 * n
	n4 := n2 * n2
	A := a / (1 + n) * (1 + n2/4 + n4/64)
	M := A * (phi -
		(3.0/2*n-9.0/16*n3)*math.Sin(2*phi) +
		(15.0/16*n2-15.0/32*n4)*math.Sin(4*phi) -
		(35.0/48*n3)*math.Sin(6*phi) +
		(315.0/512*n4)*math.Sin(8*phi))

	// p = cos(φ)·(λ−λ₀) is the key quantity in the DMA series.
	// All the TM series terms use powers of p, not powers of (λ−λ₀) directly.
	p := cosPhi * (lambda - lambda0)
	p2 := p * p
	p3 := p2 * p
	p4 := p2 * p2
	p5 := p4 * p
	p6 := p4 * p2

	// Easting (DMA TM 8358.2 eq. 2-5).
	x := k0 * nu * (p +
		p3/6*(1-t2+c) +
		p5/120*(5-18*t2+t2*t2+14*c-58*t2*ep2))

	// Northing (DMA TM 8358.2 eq. 2-6).
	y := k0 * (M + nu*t*(
		p2/2+
			p4/24*(5-t2+9*c+4*c2)+
			p6/720*(61-58*t2+t2*t2+600*c-330*ep2)))

	easting = x + 500_000.0
	northing = y
	if !northern {
		northing += 10_000_000.0
	}
	return easting, northing, zone, northern
}

// TileCenter returns the WGS84 latitude/longitude of the centre of a UTM
// bounding box.  Convenience wrapper around ToLatLon.
func TileCenter(minE, maxE, minN, maxN float64, zone int, northern bool) (latDeg, lonDeg float64) {
	return ToLatLon((minE+maxE)/2, (minN+maxN)/2, zone, northern)
}

// ZoneFromEPSG decodes the UTM zone number and hemisphere from a projected
// CRS EPSG code.  It handles the most common UTM families:
//
//	EPSG 258XX  ETRS89 / UTM Zone XX North  (e.g. 25832 → zone 32, north)
//	EPSG 326XX  WGS84  / UTM Zone XX North  (e.g. 32632 → zone 32, north)
//	EPSG 327XX  WGS84  / UTM Zone XX South  (e.g. 32732 → zone 32, south)
//
// ok is false when the EPSG code is not recognised as a UTM CRS.
func ZoneFromEPSG(epsg int) (zone int, northern bool, ok bool) {
	switch {
	case epsg >= 25801 && epsg <= 25860:
		return epsg - 25800, true, true
	case epsg >= 32601 && epsg <= 32660:
		return epsg - 32600, true, true
	case epsg >= 32701 && epsg <= 32760:
		return epsg - 32700, false, true
	default:
		return 0, false, false
	}
}

func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }
func rad2deg(r float64) float64 { return r * 180.0 / math.Pi }
