// Package geotiff reads GeoTIFF elevation rasters.
// It handles the binary TIFF structure (IFD tag parsing, tiled float32 pixels)
// and the GeoTIFF extension tags that carry georeferencing information.
package geotiff

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// TIFF data types as defined in the TIFF 6.0 spec.
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeFloat     = 11
	typeDouble    = 12
)

// TIFF and GeoTIFF tag numbers.
const (
	tagImageWidth        = 256
	tagImageLength       = 257
	tagBitsPerSample     = 258
	tagCompression       = 259
	tagSampleFormat      = 339
	tagTileWidth         = 322
	tagTileLength        = 323
	tagTileOffsets       = 324
	tagTileByteCounts    = 325
	tagModelPixelScale   = 33550
	tagModelTiepoint     = 33922
	tagGeoKeyDirectory   = 34735
	tagGeoDoubleParams   = 34736
	tagGeoAsciiParams    = 34737
	tagGDALNoData        = 42113
)

// GeoTIFF GeoKey IDs.
const (
	geoKeyProjectedCSType = 3072 // holds the EPSG projected CRS code
)

// ifdEntry represents one 12-byte IFD directory entry.
type ifdEntry struct {
	Tag    uint16
	Type   uint16
	Count  uint32
	Value  uint32 // either the value itself (if it fits in 4 bytes) or an offset
}

type tiffReader struct {
	f      *os.File
	order  binary.ByteOrder
}

// open opens a TIFF file, reads the byte-order mark and magic number,
// and returns a tiffReader ready to parse the first IFD.
func open(path string) (*tiffReader, uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	var bom [2]byte
	if _, err := io.ReadFull(f, bom[:]); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("reading byte-order mark: %w", err)
	}

	var order binary.ByteOrder
	switch string(bom[:]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		f.Close()
		return nil, 0, fmt.Errorf("unknown byte-order mark %q", bom)
	}

	var magic uint16
	if err := binary.Read(f, order, &magic); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("reading TIFF magic: %w", err)
	}
	if magic != 42 {
		f.Close()
		return nil, 0, fmt.Errorf("not a TIFF file (magic=%d)", magic)
	}

	var ifdOffset uint32
	if err := binary.Read(f, order, &ifdOffset); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("reading IFD offset: %w", err)
	}

	return &tiffReader{f: f, order: order}, ifdOffset, nil
}

// readIFD reads all IFD entries at the given file offset and returns them
// as a map keyed by tag number for easy lookup.
func (r *tiffReader) readIFD(offset uint32) (map[uint16]ifdEntry, error) {
	if _, err := r.f.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking to IFD at %d: %w", offset, err)
	}

	var numEntries uint16
	if err := binary.Read(r.f, r.order, &numEntries); err != nil {
		return nil, fmt.Errorf("reading IFD entry count: %w", err)
	}

	entries := make(map[uint16]ifdEntry, numEntries)
	for i := 0; i < int(numEntries); i++ {
		var e ifdEntry
		if err := binary.Read(r.f, r.order, &e); err != nil {
			return nil, fmt.Errorf("reading IFD entry %d: %w", i, err)
		}
		entries[e.Tag] = e
	}
	return entries, nil
}

// readShorts reads Count uint16 values from the location given by an IFD entry.
// If the values fit in the 4-byte Value field they are read from there directly;
// otherwise we seek to the offset.
func (r *tiffReader) readShorts(e ifdEntry) ([]uint16, error) {
	out := make([]uint16, e.Count)
	if e.Count*2 <= 4 {
		// Values are packed into the Value field itself.
		buf := make([]byte, 4)
		r.order.PutUint32(buf, e.Value)
		for i := range out {
			out[i] = r.order.Uint16(buf[i*2:])
		}
		return out, nil
	}
	if _, err := r.f.Seek(int64(e.Value), io.SeekStart); err != nil {
		return nil, err
	}
	return out, binary.Read(r.f, r.order, out)
}

// readLongs reads Count uint32 values from the location given by an IFD entry.
func (r *tiffReader) readLongs(e ifdEntry) ([]uint32, error) {
	out := make([]uint32, e.Count)
	if e.Count == 1 {
		out[0] = e.Value
		return out, nil
	}
	if _, err := r.f.Seek(int64(e.Value), io.SeekStart); err != nil {
		return nil, err
	}
	return out, binary.Read(r.f, r.order, out)
}

// readDoubles reads Count float64 values from the offset stored in an IFD entry.
func (r *tiffReader) readDoubles(e ifdEntry) ([]float64, error) {
	if _, err := r.f.Seek(int64(e.Value), io.SeekStart); err != nil {
		return nil, err
	}
	out := make([]float64, e.Count)
	return out, binary.Read(r.f, r.order, out)
}

// readASCII reads a null-terminated ASCII string from the offset in an IFD entry.
func (r *tiffReader) readASCII(e ifdEntry) (string, error) {
	if _, err := r.f.Seek(int64(e.Value), io.SeekStart); err != nil {
		return "", err
	}
	buf := make([]byte, e.Count)
	if _, err := io.ReadFull(r.f, buf); err != nil {
		return "", err
	}
	// Trim trailing nulls — TIFF ASCII fields end with a null byte.
	for len(buf) > 0 && buf[len(buf)-1] == 0 {
		buf = buf[:len(buf)-1]
	}
	return string(buf), nil
}

func singleShort(e ifdEntry) uint16 {
	return uint16(e.Value)
}

// readTiledFloat32 assembles the full raster from TIFF tiles into a flat
// row-major []float32 slice of length width*height.
// noDataValue, if not NaN, marks pixels to treat as missing.
func (r *tiffReader) readTiledFloat32(
	entries map[uint16]ifdEntry,
	width, height int,
	noDataValue float32,
) ([]float32, error) {
	tileWidth := int(singleShort(entries[tagTileWidth]))
	tileHeight := int(singleShort(entries[tagTileLength]))

	offsets, err := r.readLongs(entries[tagTileOffsets])
	if err != nil {
		return nil, fmt.Errorf("reading tile offsets: %w", err)
	}
	byteCounts, err := r.readLongs(entries[tagTileByteCounts])
	if err != nil {
		return nil, fmt.Errorf("reading tile byte counts: %w", err)
	}

	tilesAcross := (width + tileWidth - 1) / tileWidth
	tilesDown := (height + tileHeight - 1) / tileHeight

	pixels := make([]float32, width*height)

	for ty := 0; ty < tilesDown; ty++ {
		for tx := 0; tx < tilesAcross; tx++ {
			idx := ty*tilesAcross + tx

			if _, err := r.f.Seek(int64(offsets[idx]), io.SeekStart); err != nil {
				return nil, fmt.Errorf("seeking to tile %d: %w", idx, err)
			}

			numPixels := int(byteCounts[idx]) / 4
			tile := make([]float32, numPixels)
			if err := binary.Read(r.f, r.order, tile); err != nil {
				return nil, fmt.Errorf("reading tile %d pixels: %w", idx, err)
			}

			originX := tx * tileWidth
			originY := ty * tileHeight
			tileW := min(tileWidth, width-originX)
			tileH := min(tileHeight, height-originY)

			for row := 0; row < tileH; row++ {
				for col := 0; col < tileW; col++ {
					tilePixel := row*tileWidth + col
					imgPixel := (originY+row)*width + (originX + col)
					pixels[imgPixel] = tile[tilePixel]
				}
			}
		}
	}

	return pixels, nil
}

// parseGeoKeys extracts GeoTIFF keys from the GeoKeyDirectoryTag IFD entry.
// It returns a map from GeoKey ID to its integer value.
// Keys stored as SHORT values in the directory itself are returned directly;
// keys that point into GeoDoubleParams are not returned here.
func (r *tiffReader) parseGeoKeys(entries map[uint16]ifdEntry) (map[uint16]uint16, error) {
	e, ok := entries[tagGeoKeyDirectory]
	if !ok {
		return nil, nil
	}

	shorts, err := r.readShorts(e)
	if err != nil {
		return nil, fmt.Errorf("reading GeoKeyDirectory: %w", err)
	}
	if len(shorts) < 4 {
		return nil, fmt.Errorf("GeoKeyDirectory too short")
	}

	numKeys := int(shorts[3])
	keys := make(map[uint16]uint16, numKeys)

	for i := 0; i < numKeys; i++ {
		base := 4 + i*4
		if base+3 >= len(shorts) {
			break
		}
		keyID := shorts[base]
		tiffTagLoc := shorts[base+1]
		valueOffset := shorts[base+3]

		// tiffTagLoc == 0 means the value is in valueOffset directly.
		if tiffTagLoc == 0 {
			keys[keyID] = valueOffset
		}
		// Other tiffTagLoc values point into GeoDoubleParams or GeoAsciiParams —
		// we don't need those for our use case.
	}

	return keys, nil
}

func float32FromBits(b uint32) float32 {
	return math.Float32frombits(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
