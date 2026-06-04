// Package api implements the HTTP handlers for the shadow analysis service.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/optimuscrime/sunspot-core/internal/render"
	"github.com/optimuscrime/sunspot-core/internal/resterr"
	"github.com/optimuscrime/sunspot-core/internal/shadow"
)

// Server holds the long-lived state shared across all requests: the tile
// indices built once at startup and reused for every query.
type Server struct {
	domIndex *shadow.TileIndex
	dtmIndex *shadow.TileIndex
	opts     shadow.Options
}

// NewServer creates a Server from pre-built tile indices.
func NewServer(dom, dtm *shadow.TileIndex, opts shadow.Options) *Server {
	return &Server{
		domIndex: dom,
		dtmIndex: dtm,
		opts:     opts,
	}
}

func (s *Server) Routes(r chi.Router) {
	r.Post("/shadow", s.handleShadow)
}

func (s *Server) handleShadow(w http.ResponseWriter, r *http.Request) {
	var req ShadowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.JSON(w, r, resterr.New("invalid JSON: "+err.Error(), http.StatusBadRequest))
		return
	}

	if err := validateRequest(req); err != nil {
		render.JSON(w, r, resterr.FromErr(err, http.StatusBadRequest))
		return
	}

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		render.JSON(w, r, resterr.New("date must be YYYY-MM-DD, got: "+req.Date, http.StatusBadRequest))
		return
	}
	// Use noon UTC on that date so that suncalc uses the right calendar day.
	date = time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, time.UTC)

	conditions, err := shadow.DayConditions(date, req.Lat, req.Lon, s.domIndex, s.dtmIndex, s.opts)
	if err != nil {
		if errors.Is(err, shadow.ErrOutOfCoverage) {
			render.JSON(w, r, resterr.FromErr(err, http.StatusNotFound))
			return
		}
		render.JSON(w, r, resterr.FromErr(err, http.StatusInternalServerError))
		return
	}

	render.JSON(w, r, buildResponse(conditions))
}

func buildResponse(conditions []shadow.Condition) ShadowResponse {
	segments := shadow.Segments(conditions)

	resp := ShadowResponse{
		MinutesInSun:   shadow.SunMinutes(conditions),
		MinutesInShade: len(conditions) - shadow.SunMinutes(conditions),
		Segments:       make([]SegmentResponse, len(segments)),
	}

	if len(conditions) > 0 {
		resp.Sunrise = conditions[0].Time
		resp.Sunset = conditions[len(conditions)-1].Time
	}

	for i, seg := range segments {
		sr := SegmentResponse{
			From:        seg.From,
			To:          seg.To,
			Minutes:     seg.Minutes,
			SunAzimuth:  seg.SunAtStart.Azimuth,
			SunAltitude: seg.SunAtStart.Altitude,
		}
		if seg.InSun {
			sr.State = "sun"
		} else {
			sr.State = "shade"
		}
		if b := seg.BlockedBy; b != nil {
			sr.BlockedBy = &BlockedByResponse{
				Lat:           b.Lat,
				Lon:           b.Lon,
				DistanceM:     b.Distance,
				SurfaceHeight: float64(b.SurfaceHeight),
			}
		}
		resp.Segments[i] = sr
	}

	return resp
}

func validateRequest(req ShadowRequest) error {
	if req.Lat < -90 || req.Lat > 90 {
		return fmt.Errorf("lat must be between -90 and 90, got %.6f", req.Lat)
	}
	if req.Lon < -180 || req.Lon > 180 {
		return fmt.Errorf("lon must be between -180 and 180, got %.6f", req.Lon)
	}
	if req.Date == "" {
		return fmt.Errorf("date is required")
	}
	return nil
}
