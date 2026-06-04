package shadow

import (
	"fmt"
	"math"
	"path/filepath"
	"sync"

	"github.com/optimuscrime/sunspot-core/internal/dataset"
	"github.com/optimuscrime/sunspot-core/internal/geotiff"
)

// tileKey identifies a tile by its grid position.
// Because all tiles in our datasets are the same size, every UTM coordinate
// maps to exactly one grid cell.
type tileKey struct{ col, row int }

// TileIndex provides O(1) coordinate → elevation lookups across a set of
// same-type tiles (all DOM or all DTM — never mixed).
//
// Pixel data is loaded from disk lazily on first access and kept in memory
// permanently after that.  The index is safe for concurrent use.
type TileIndex struct {
	cells map[tileKey]*indexCell

	// stepE / stepN are the tile width and height in metres.
	// Derived once at build time from the first tile.
	stepE float64
	stepN float64
}

// indexCell holds one tile and ensures its pixels are loaded at most once,
// even under concurrent access.
type indexCell struct {
	entry  *dataset.TileEntry
	once   sync.Once
	pixels []float32 // set by once.Do; nil until first access
	err    error
}

// IndexStats describes how many tiles from each source dataset were actually
// registered in the index (after deduplication).
type IndexStats struct {
	TilesBySource map[string]int // dataset root path → tile count
	TotalCells    int
}

// NewTileIndex builds an index from a flat slice of tile entries.
//
// When two entries cover the same grid cell the first one wins, so callers
// should pass higher-priority tiles (e.g. from newer surveys) first — see
// dataset.MergeTiles.
func NewTileIndex(entries []*dataset.TileEntry) (*TileIndex, *IndexStats, error) {
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no tiles provided")
	}

	first := entries[0].Tile
	minE, maxE := first.BoundsE()
	minN, maxN := first.BoundsN()

	idx := &TileIndex{
		cells: make(map[tileKey]*indexCell, len(entries)),
		stepE: maxE - minE,
		stepN: maxN - minN,
	}
	stats := &IndexStats{TilesBySource: make(map[string]int)}

	for _, e := range entries {
		key := idx.keyForTile(e.Tile)
		if _, exists := idx.cells[key]; exists {
			continue // a higher-priority tile already occupies this cell
		}
		idx.cells[key] = &indexCell{entry: e}
		// Label each tile by its dataset root (two levels above data/dom/<file>).
		source := filepath.Dir(filepath.Dir(filepath.Dir(e.Tile.Path)))
		stats.TilesBySource[source]++
	}
	stats.TotalCells = len(idx.cells)

	return idx, stats, nil
}

// ElevationAt returns the surface elevation in metres at UTM coordinate (e, n).
// Returns (0, false) when the coordinate falls outside all registered tiles.
func (idx *TileIndex) ElevationAt(e, n float64) (float32, bool) {
	cell, ok := idx.cells[idx.keyForCoord(e, n)]
	if !ok {
		return 0, false
	}

	pixels, err := cell.load()
	if err != nil || pixels == nil {
		return 0, false
	}

	t := cell.entry.Tile
	col := int((e - t.Georef.OriginE) / t.Georef.PixelWidth)
	row := int((t.Georef.OriginN - n) / t.Georef.PixelHeight)
	if col < 0 || col >= t.Width || row < 0 || row >= t.Height {
		return 0, false
	}

	return pixels[row*t.Width+col], true
}

// load returns the pixel slice for this cell, loading from disk on first call.
// Subsequent calls return the cached result immediately.
func (cell *indexCell) load() ([]float32, error) {
	cell.once.Do(func() {
		if err := cell.entry.Tile.LoadPixels(); err != nil {
			cell.err = err
			return
		}
		cell.pixels = cell.entry.Tile.Pixels
	})
	return cell.pixels, cell.err
}

// keyForTile computes the grid key for a tile using its origin corner.
// We use minE (left edge) for the column and maxN (top edge) for the row
// because that is how tiles are positioned in image space.
func (idx *TileIndex) keyForTile(t *geotiff.Tile) tileKey {
	minE, _ := t.BoundsE()
	_, maxN := t.BoundsN()
	return tileKey{
		col: int(math.Round(minE / idx.stepE)),
		row: int(math.Round(maxN / idx.stepN)),
	}
}

// keyForCoord maps an arbitrary UTM coordinate to the grid cell that contains it.
// The rounding is chosen to be consistent with keyForTile:
//
//	col = Floor(e/stepE)  matches  Round(minE/stepE) for any e in [minE, minE+stepE)
//	row = Ceil(n/stepN)   matches  Round(maxN/stepN) for any n in (maxN-stepN, maxN]
func (idx *TileIndex) keyForCoord(e, n float64) tileKey {
	return tileKey{
		col: int(math.Floor(e / idx.stepE)),
		row: int(math.Ceil(n / idx.stepN)),
	}
}
