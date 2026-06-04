// Package dataset discovers and loads GIS elevation datasets from a directory tree.
//
// It expects a layout like:
//
//	<root>/
//	  <dataset name>/
//	    data/
//	      dom/   ← Digital Surface Model tiles
//	      dtm/   ← Digital Terrain Model tiles
//	    metadata/
package dataset

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/optimuscrime/sunspot-core/internal/geotiff"
	"github.com/optimuscrime/sunspot-core/internal/sosi"
)

// TileType distinguishes DOM (surface model) from DTM (terrain model).
type TileType string

const (
	DOM TileType = "dom"
	DTM TileType = "dtm"
)

// TileEntry groups a loaded GeoTIFF tile with its SOSI metadata and type.
type TileEntry struct {
	Type     TileType
	SOSIMeta *sosi.RasterMetadata // may be nil if no .sos file was found
	Tile     *geotiff.Tile
}

// Dataset represents one named collection of elevation tiles (e.g. one survey).
type Dataset struct {
	Name string
	Root string // absolute path to the dataset directory

	Tiles []*TileEntry
}

func (d *Dataset) DOMTiles() []*TileEntry {
	return d.tilesOfType(DOM)
}

func (d *Dataset) DTMTiles() []*TileEntry {
	return d.tilesOfType(DTM)
}

func (d *Dataset) tilesOfType(t TileType) []*TileEntry {
	var result []*TileEntry
	for _, e := range d.Tiles {
		if e.Type == t {
			result = append(result, e)
		}
	}
	return result
}

// LoadOptions controls which files are loaded.
type LoadOptions struct {
	// LoadPixels controls whether the full float32 pixel array is read into memory.
	// Set to false to load only metadata (faster for indexing large datasets).
	LoadPixels bool
}

// Discover scans root for dataset sub-directories and loads all tiles
// according to opts.  Each immediate sub-directory of root is treated as
// one dataset.
func Discover(root string, opts LoadOptions) ([]*Dataset, error) {
	entries, err := fs.ReadDir(filesystemAt(root), ".")
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", root, err)
	}

	var datasets []*Dataset
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		datasetPath := filepath.Join(root, entry.Name())
		ds, err := loadDataset(datasetPath, entry.Name(), opts)
		if err != nil {
			return nil, fmt.Errorf("loading dataset %q: %w", entry.Name(), err)
		}
		datasets = append(datasets, ds)
	}

	return datasets, nil
}

func loadDataset(root, name string, opts LoadOptions) (*Dataset, error) {
	ds := &Dataset{Name: name, Root: root}

	for _, tileType := range []TileType{DOM, DTM} {
		dir := filepath.Join(root, "data", string(tileType))
		tiles, err := loadTilesFromDir(dir, tileType, opts)
		if err != nil {
			// Missing dom/dtm subdirectory is not fatal — just skip it.
			continue
		}
		ds.Tiles = append(ds.Tiles, tiles...)
	}

	return ds, nil
}

func loadTilesFromDir(dir string, tileType TileType, opts LoadOptions) ([]*TileEntry, error) {
	pattern := filepath.Join(dir, "*.tif")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Glob returns nil (not an error) when nothing matches — treat as empty.
	var entries []*TileEntry
	for _, tifPath := range matches {
		// Skip .ovr overview files that end with .tif via a symlink or renaming.
		if strings.HasSuffix(tifPath, ".ovr") {
			continue
		}

		entry, err := loadTileEntry(tifPath, tileType, opts)
		if err != nil {
			return nil, fmt.Errorf("loading tile %s: %w", tifPath, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func loadTileEntry(tifPath string, tileType TileType, opts LoadOptions) (*TileEntry, error) {
	sosPath := strings.TrimSuffix(tifPath, ".tif") + ".sos"
	sosMeta, _ := sosi.Read(sosPath) // ignore error; .sos is optional

	var tile *geotiff.Tile
	var err error

	if opts.LoadPixels {
		tile, err = geotiff.Read(tifPath)
	} else {
		tile, err = geotiff.ReadHeader(tifPath)
	}
	if err != nil {
		return nil, err
	}

	return &TileEntry{
		Type:     tileType,
		SOSIMeta: sosMeta,
		Tile:     tile,
	}, nil
}

// MergeTiles returns a flat list of tiles of the given type from all datasets,
// sorted so that tiles from the most recently named dataset come first.
//
// Because the TileIndex uses "first registration wins" for duplicate cells,
// this means newer surveys take priority over older ones where they overlap,
// while older data fills in any gaps the newer survey does not cover.
//
// The sort is lexicographic on the dataset name, which works correctly for
// year-suffixed names like "Oslo survey 2014" and "Oslo survey 2019".
func MergeTiles(datasets []*Dataset, t TileType) []*TileEntry {
	sorted := make([]*Dataset, len(datasets))
	copy(sorted, datasets)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name > sorted[j].Name
	})

	var tiles []*TileEntry
	for _, ds := range sorted {
		tiles = append(tiles, ds.tilesOfType(t)...)
	}
	return tiles
}

func filesystemAt(root string) fs.FS {
	return os.DirFS(root)
}
