package api

import "time"

// ShadowRequest is the JSON body for POST /shadow.
type ShadowRequest struct {
	Lat  float64 `json:"lat"`  // WGS84 latitude in decimal degrees
	Lon  float64 `json:"lon"`  // WGS84 longitude in decimal degrees
	Date string  `json:"date"` // calendar date in "YYYY-MM-DD" format (UTC)
}

// ShadowResponse is returned by POST /shadow on success.
type ShadowResponse struct {
	Sunrise        time.Time         `json:"sunrise"`          // first sun above min altitude
	Sunset         time.Time         `json:"sunset"`           // last sun above min altitude
	MinutesInSun   int               `json:"minutes_in_sun"`
	MinutesInShade int               `json:"minutes_in_shade"`
	Segments       []SegmentResponse `json:"segments"`
}

// SegmentResponse describes one consecutive run of sun or shade.
type SegmentResponse struct {
	From        time.Time        `json:"from"`         // UTC, inclusive
	To          time.Time        `json:"to"`           // UTC, inclusive
	State       string           `json:"state"`        // "sun" or "shade"
	Minutes     int              `json:"minutes"`
	SunAzimuth  float64          `json:"sun_azimuth"`  // degrees clockwise from North
	SunAltitude float64          `json:"sun_altitude"` // degrees above horizon
	BlockedBy   *BlockedByResponse `json:"blocked_by,omitempty"`
}

// BlockedByResponse describes the obstacle that caused a shade segment.
type BlockedByResponse struct {
	Lat           float64 `json:"lat"`               // WGS84 latitude of blocking pixel
	Lon           float64 `json:"lon"`               // WGS84 longitude
	DistanceM     float64 `json:"distance_m"`        // horizontal metres from observer
	SurfaceHeight float64 `json:"surface_height_m"`  // DOM elevation in metres
}

