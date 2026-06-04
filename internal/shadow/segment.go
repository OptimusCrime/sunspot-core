package shadow

import (
	"time"

	"github.com/optimuscrime/sunspot-core/internal/sun"
)

// Segment is a consecutive run of the same sun/shade state.
type Segment struct {
	From    time.Time
	To      time.Time
	InSun   bool
	Minutes int

	// SunAtStart is the sun position at the first minute of the segment.
	SunAtStart sun.Position

	// BlockedBy is the first blocking structure encountered at the start of
	// this segment.  Nil for sun segments and for shade-below-horizon segments.
	BlockedBy *BlockPoint
}

// Segments groups a flat []Condition slice into consecutive same-state runs.
// The input must be sorted chronologically (as returned by DayConditions).
func Segments(conditions []Condition) []Segment {
	if len(conditions) == 0 {
		return nil
	}

	var segments []Segment

	start := conditions[0]
	count := 1

	for i := 1; i < len(conditions); i++ {
		if conditions[i].InSun == conditions[i-1].InSun {
			count++
			continue
		}
		segments = append(segments, Segment{
			From:       start.Time,
			To:         conditions[i-1].Time,
			InSun:      start.InSun,
			Minutes:    count,
			SunAtStart: start.Sun,
			BlockedBy:  start.BlockedBy,
		})
		start = conditions[i]
		count = 1
	}

	// Flush the final segment.
	segments = append(segments, Segment{
		From:       start.Time,
		To:         conditions[len(conditions)-1].Time,
		InSun:      start.InSun,
		Minutes:    count,
		SunAtStart: start.Sun,
		BlockedBy:  start.BlockedBy,
	})

	return segments
}

func SunMinutes(conditions []Condition) int {
	n := 0
	for _, c := range conditions {
		if c.InSun {
			n++
		}
	}
	return n
}
