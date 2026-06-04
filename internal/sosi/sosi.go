// Package sosi parses the Norwegian SOSI geographic metadata format (.sos files).
//
// SOSI (Samordnet Opplegg for Stedfestet Informasjon) is a hierarchical
// text-based format where objects are introduced by dot-prefixed keywords,
// and nesting depth is indicated by the number of dots.
//
// Example:
//
//	.HODE
//	..TRANSPAR
//	...KOORDSYS 22
//	...ENHET 0.01
//	..OMRÅDE
//	...MIN-NØ 6642000 591200
//	...MAX-NØ 6642600 592000
//	.RASTER 1
//	..BILDE
//	...BILDE-FIL "filename.tif"
//	...PIXEL-STØRR 1.0 1.0
package sosi

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// RasterMetadata holds the fields from a SOSI raster (.sos) file that are
// relevant for locating and interpreting the associated GeoTIFF.
type RasterMetadata struct {
	// CoordSys is the SOSI coordinate system code (22 = UTM Zone 32N).
	CoordSys int

	// Unit is the multiplier for raw coordinate values (typically 0.01).
	Unit float64

	// MinN, MinE, MaxN, MaxE are the bounding box corners in metres,
	// already scaled by Unit.
	MinN, MinE float64
	MaxN, MaxE float64

	// SOSIVersion is the SOSI format version string, e.g. "4.1".
	SOSIVersion string

	// Producer is the organisation that generated the data.
	Producer string

	// ImageFile is the base filename of the associated raster.
	ImageFile string

	// BitsPerPixel is the raster bit depth (typically 32 for float32).
	BitsPerPixel int

	// PixelSizeX and PixelSizeY are the pixel dimensions in metres.
	PixelSizeX, PixelSizeY float64
}

func Read(path string) (*RasterMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	meta := &RasterMetadata{Unit: 1.0} // default unit is 1 (metres)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Comment lines start with '!'
		if strings.HasPrefix(line, "!") {
			continue
		}

		keyword, rest := splitKeyword(line)

		switch keyword {
		case "KOORDSYS":
			meta.CoordSys = parseInt(rest)
		case "ENHET":
			meta.Unit = parseFloat(rest)
		case "SOSI-VERSJON":
			meta.SOSIVersion = rest
		case "PRODUSENT":
			meta.Producer = strings.Trim(rest, `"`)
		case "MIN-NØ":
			n, e := parseTwoFloats(rest)
			meta.MinN = n * meta.Unit
			meta.MinE = e * meta.Unit
		case "MAX-NØ":
			n, e := parseTwoFloats(rest)
			meta.MaxN = n * meta.Unit
			meta.MaxE = e * meta.Unit
		case "BILDE-BIT-PIXEL":
			meta.BitsPerPixel = parseInt(rest)
		case "BILDE-FIL":
			meta.ImageFile = strings.Trim(rest, `"`)
		case "PIXEL-STØRR":
			meta.PixelSizeX, meta.PixelSizeY = parseTwoFloats(rest)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return meta, nil
}

// splitKeyword splits a SOSI line into its keyword (stripped of leading dots)
// and its value string.  "...MIN-NØ 6642000 591200" → ("MIN-NØ", "6642000 591200").
func splitKeyword(line string) (keyword, rest string) {
	line = strings.TrimLeft(line, ".")
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSpace(parts[1])
}

func parseInt(s string) int {
	s = strings.Fields(s)[0]
	v, _ := strconv.Atoi(s)
	return v
}

func parseFloat(s string) float64 {
	s = strings.Fields(s)[0]
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parseTwoFloats parses two whitespace-separated float values from a string.
// Returns (0, 0) if parsing fails.
func parseTwoFloats(s string) (float64, float64) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0, 0
	}
	a, _ := strconv.ParseFloat(fields[0], 64)
	b, _ := strconv.ParseFloat(fields[1], 64)
	return a, b
}
