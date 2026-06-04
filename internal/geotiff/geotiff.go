package geotiff

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// Georef holds the affine transform that maps pixel coordinates to
// real-world projected coordinates (Easting / Northing in meters).
//
// World coordinate from pixel (col, row):
//
//	E = OriginE + float64(col)*PixelWidth
//	N = OriginN - float64(row)*PixelHeight   (Y decreases down the image)
type Georef struct {
	OriginE     float64 // Easting of the top-left pixel corner
	OriginN     float64 // Northing of the top-left pixel corner
	PixelWidth  float64 // metres per pixel in X
	PixelHeight float64 // metres per pixel in Y (always positive; Y flips in image space)
	EPSG        int     // projected CRS, e.g. 25832 for ETRS89/UTM32N
}

func (g Georef) EastingNorthing(col, row int) (easting, northing float64) {
	return g.OriginE + float64(col)*g.PixelWidth,
		g.OriginN - float64(row)*g.PixelHeight
}

// Statistics holds the band statistics stored in the GDAL PAM sidecar file.
type Statistics struct {
	Min    float64
	Max    float64
	Mean   float64
	StdDev float64
}

// Tile is a single DOM or DTM GeoTIFF tile with its elevation pixels and
// all associated metadata.
type Tile struct {
	Path   string
	Width  int
	Height int

	Georef Georef

	// NoDataValue is the sentinel float32 that marks missing / invalid pixels.
	// math.NaN() is used when the file does not specify one.
	NoDataValue float32

	// Pixels is a flat row-major slice of elevation values in metres.
	// Index a pixel at (col, row) as Pixels[row*Width+col].
	Pixels []float32

	// Stats may be nil if no .aux.xml sidecar was found.
	Stats *Statistics
}

// LoadPixels reads the full float32 pixel array into t.Pixels.
// It is a no-op if pixels are already loaded.
func (t *Tile) LoadPixels() error {
	if t.Pixels != nil {
		return nil
	}
	full, err := Read(t.Path)
	if err != nil {
		return err
	}
	t.Pixels = full.Pixels
	return nil
}

func (t *Tile) BoundsE() (min, max float64) {
	return t.Georef.OriginE, t.Georef.OriginE + float64(t.Width)*t.Georef.PixelWidth
}

func (t *Tile) BoundsN() (min, max float64) {
	maxN := t.Georef.OriginN
	minN := t.Georef.OriginN - float64(t.Height)*t.Georef.PixelHeight
	return minN, maxN
}

// ElevationAt returns the elevation in metres at pixel (col, row).
// It returns false if the pixel is out of bounds or holds the NoData value.
func (t *Tile) ElevationAt(col, row int) (float64, bool) {
	if col < 0 || col >= t.Width || row < 0 || row >= t.Height {
		return 0, false
	}
	v := t.Pixels[row*t.Width+col]
	if isNoData(v, t.NoDataValue) {
		return 0, false
	}
	return float64(v), true
}

// isNoData reports whether pixel value v matches the tile's nodata sentinel.
// NaN is handled explicitly because NaN != NaN in IEEE 754.
func isNoData(v, noData float32) bool {
	if math.IsNaN(float64(noData)) {
		return math.IsNaN(float64(v))
	}
	return v == noData
}

// ReadHeader opens a GeoTIFF file and parses only the header tags (dimensions,
// georeferencing, statistics). No pixel data is read into memory.
// Use this when you need to index or inspect a large number of tiles cheaply.
func ReadHeader(path string) (*Tile, error) {
	r, ifdOffset, err := open(path)
	if err != nil {
		return nil, err
	}
	defer r.f.Close()

	entries, err := r.readIFD(ifdOffset)
	if err != nil {
		return nil, fmt.Errorf("parsing IFD: %w", err)
	}

	width := int(entries[tagImageWidth].Value)
	height := int(entries[tagImageLength].Value)

	noData := float32(math.NaN())
	if e, ok := entries[tagGDALNoData]; ok {
		s, err := r.readASCII(e)
		if err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(s), 32); err == nil {
				noData = float32(v)
			}
		}
	}

	georef, err := r.readGeoref(entries)
	if err != nil {
		return nil, fmt.Errorf("reading georeferencing: %w", err)
	}

	tile := &Tile{
		Path:        path,
		Width:       width,
		Height:      height,
		Georef:      georef,
		NoDataValue: noData,
	}
	tile.Stats, _ = readAuxXML(path + ".aux.xml")
	return tile, nil
}

// Read opens a GeoTIFF file, parses its georeferencing tags, reads all
// elevation pixels into memory, and optionally loads statistics from the
// GDAL PAM sidecar (<path>.aux.xml).
func Read(path string) (*Tile, error) {
	r, ifdOffset, err := open(path)
	if err != nil {
		return nil, err
	}
	defer r.f.Close()

	entries, err := r.readIFD(ifdOffset)
	if err != nil {
		return nil, fmt.Errorf("parsing IFD: %w", err)
	}

	width := int(entries[tagImageWidth].Value)
	height := int(entries[tagImageLength].Value)

	noData := float32(math.NaN())
	if e, ok := entries[tagGDALNoData]; ok {
		s, err := r.readASCII(e)
		if err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(s), 32); err == nil {
				noData = float32(v)
			}
		}
	}

	georef, err := r.readGeoref(entries)
	if err != nil {
		return nil, fmt.Errorf("reading georeferencing: %w", err)
	}

	pixels, err := r.readTiledFloat32(entries, width, height, noData)
	if err != nil {
		return nil, fmt.Errorf("reading pixel data: %w", err)
	}

	tile := &Tile{
		Path:        path,
		Width:       width,
		Height:      height,
		Georef:      georef,
		NoDataValue: noData,
		Pixels:      pixels,
	}

	tile.Stats, _ = readAuxXML(path + ".aux.xml")

	return tile, nil
}

func (r *tiffReader) readGeoref(entries map[uint16]ifdEntry) (Georef, error) {
	var georef Georef

	// ModelPixelScaleTag: (scaleX, scaleY, scaleZ) — metres per pixel.
	if e, ok := entries[tagModelPixelScale]; ok {
		scales, err := r.readDoubles(e)
		if err != nil {
			return georef, fmt.Errorf("reading ModelPixelScale: %w", err)
		}
		if len(scales) >= 2 {
			georef.PixelWidth = scales[0]
			georef.PixelHeight = scales[1]
		}
	}

	// ModelTiepointTag: (i, j, k, x, y, z) — pixel (i,j) maps to world (x,y).
	if e, ok := entries[tagModelTiepoint]; ok {
		pts, err := r.readDoubles(e)
		if err != nil {
			return georef, fmt.Errorf("reading ModelTiepoint: %w", err)
		}
		if len(pts) >= 6 {
			// i,j are almost always 0,0 (top-left pixel).
			// x,y are the corresponding real-world Easting, Northing.
			// We adjust by i,j in case they are non-zero.
			georef.OriginE = pts[3] - pts[0]*georef.PixelWidth
			georef.OriginN = pts[4] + pts[1]*georef.PixelHeight
		}
	}

	// GeoKeyDirectoryTag: parse the projected CRS EPSG code.
	geoKeys, err := r.parseGeoKeys(entries)
	if err != nil {
		return georef, fmt.Errorf("parsing GeoKeys: %w", err)
	}
	if epsg, ok := geoKeys[geoKeyProjectedCSType]; ok {
		georef.EPSG = int(epsg)
	}

	return georef, nil
}

// auxXML mirrors the relevant parts of the GDAL PAM XML structure.
type auxXML struct {
	RasterBands []auxRasterBand `xml:"PAMRasterBand"`
}

type auxRasterBand struct {
	Metadata []auxMDI `xml:"Metadata>MDI"`
}

type auxMDI struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// readAuxXML parses the GDAL PAM sidecar file for per-band statistics.
func readAuxXML(path string) (*Statistics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var aux auxXML
	if err := xml.Unmarshal(data, &aux); err != nil {
		return nil, err
	}
	if len(aux.RasterBands) == 0 {
		return nil, nil
	}

	stats := &Statistics{}
	for _, mdi := range aux.RasterBands[0].Metadata {
		v, err := strconv.ParseFloat(strings.TrimSpace(mdi.Value), 64)
		if err != nil {
			continue
		}
		switch mdi.Key {
		case "STATISTICS_MINIMUM":
			stats.Min = v
		case "STATISTICS_MAXIMUM":
			stats.Max = v
		case "STATISTICS_MEAN":
			stats.Mean = v
		case "STATISTICS_STDDEV":
			stats.StdDev = v
		}
	}
	return stats, nil
}
