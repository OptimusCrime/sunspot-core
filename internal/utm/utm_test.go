package utm

import (
	"math"
	"testing"
)

// Round-trip tolerance: 1e-7° ≈ 0.01 mm at the equator, well within the
// stated sub-millimetre accuracy of the Redfearn series.
const roundTripTolerance = 1e-7 // degrees

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		lat  float64
		lon  float64
		zone int
	}{
		{"Oslo area", 59.9139, 10.7522, 32},
		{"Oslo east", 59.85, 10.95, 32},
		{"Tromsø", 69.6489, 18.9551, 33},
		{"Bergen", 60.3913, 5.3221, 32},
		{"Central meridian (zone 32)", 45.0, 9.0, 32},
		{"Equator (zone 32)", 0.0, 9.0, 32},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Forward: lat/lon → UTM
			e, n, zone, north := FromLatLon(c.lat, c.lon, c.zone)

			// Inverse: UTM → lat/lon
			gotLat, gotLon := ToLatLon(e, n, zone, north)

			dLat := math.Abs(gotLat - c.lat)
			dLon := math.Abs(gotLon - c.lon)

			if dLat > roundTripTolerance {
				t.Errorf("lat round-trip error: %.6f° → E=%.1f N=%.1f → %.6f°  (Δ=%.3e°)",
					c.lat, e, n, gotLat, dLat)
			}
			if dLon > roundTripTolerance {
				t.Errorf("lon round-trip error: %.6f° → E=%.1f N=%.1f → %.6f°  (Δ=%.3e°)",
					c.lon, e, n, gotLon, dLon)
			}
		})
	}
}

// TestKnownPoint checks a single well-known case against a hand-verified
// reference: the central meridian of zone 32 at the equator must give
// E=500000, N=0 and must invert back to lat=0, lon=9 exactly.
func TestKnownPoint(t *testing.T) {
	e, n, zone, north := FromLatLon(0.0, 9.0, 32)
	if math.Abs(e-500_000.0) > 1e-3 {
		t.Errorf("easting: got %.6f, want 500000.000", e)
	}
	if math.Abs(n) > 1e-3 {
		t.Errorf("northing: got %.6f, want 0.000", n)
	}
	if zone != 32 || !north {
		t.Errorf("zone/hemisphere: got %d north=%v, want 32 true", zone, north)
	}

	lat, lon := ToLatLon(500_000, 0, 32, true)
	if math.Abs(lat) > 1e-9 {
		t.Errorf("inverse lat: got %.10f°, want 0", lat)
	}
	if math.Abs(lon-9.0) > 1e-9 {
		t.Errorf("inverse lon: got %.10f°, want 9.0", lon)
	}
}

func TestZoneFromEPSG(t *testing.T) {
	cases := []struct {
		epsg         int
		wantZone     int
		wantNorthern bool
		wantOK       bool
	}{
		{25832, 32, true, true},  // ETRS89 / UTM Zone 32N
		{25833, 33, true, true},  // ETRS89 / UTM Zone 33N
		{32632, 32, true, true},  // WGS84 / UTM Zone 32N
		{32732, 32, false, true}, // WGS84 / UTM Zone 32S
		{4326, 0, false, false},  // WGS84 geographic — not UTM
	}

	for _, c := range cases {
		zone, northern, ok := ZoneFromEPSG(c.epsg)
		if ok != c.wantOK || zone != c.wantZone || northern != c.wantNorthern {
			t.Errorf("ZoneFromEPSG(%d): got (%d, %v, %v), want (%d, %v, %v)",
				c.epsg, zone, northern, ok, c.wantZone, c.wantNorthern, c.wantOK)
		}
	}
}
