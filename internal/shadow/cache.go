package shadow

import (
	"container/list"
	"sync"

	"github.com/optimuscrime/sunspot-core/internal/geotiff"
)

// TileCache is a shared, byte-budgeted LRU cache of decoded tile pixel
// arrays, keyed by file path. It is shared across DOM and DTM tile
// indices so that memory use is bounded by a single budget regardless of
// which mix of tile types is actually queried.
//
// Without a cache like this, a TileIndex that keeps every tile it has
// ever loaded would grow until it held the entire dataset in memory. The
// cache instead evicts the least-recently-used tile whenever the budget
// is exceeded; an evicted tile is simply re-read from local disk the next
// time it is queried.
type TileCache struct {
	mu       sync.Mutex
	maxBytes int64
	curBytes int64
	ll       *list.List               // front = most recently used
	elems    map[string]*list.Element // tile path -> element holding *cacheEntry
}

type cacheEntry struct {
	path   string
	pixels []float32
	bytes  int64
}

// NewTileCache creates a cache that evicts tiles once the total size of
// resident pixel arrays exceeds maxBytes.
func NewTileCache(maxBytes int64) *TileCache {
	return &TileCache{
		maxBytes: maxBytes,
		ll:       list.New(),
		elems:    make(map[string]*list.Element),
	}
}

// Pixels returns the pixel array for the tile at path, reading it from
// disk on a cache miss. The returned slice must not be mutated by
// callers, since it is shared and may be handed out again on a later hit.
func (c *TileCache) Pixels(path string) ([]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.elems[path]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*cacheEntry).pixels, nil
	}

	tile, err := geotiff.Read(path)
	if err != nil {
		return nil, err
	}

	entry := &cacheEntry{
		path:   path,
		pixels: tile.Pixels,
		bytes:  int64(len(tile.Pixels)) * 4, // float32
	}
	c.elems[path] = c.ll.PushFront(entry)
	c.curBytes += entry.bytes

	c.evictLocked()

	return entry.pixels, nil
}

// evictLocked drops least-recently-used entries until curBytes is back
// within budget. Must be called with c.mu held.
func (c *TileCache) evictLocked() {
	for c.curBytes > c.maxBytes {
		oldest := c.ll.Back()
		if oldest == nil {
			return
		}
		c.ll.Remove(oldest)
		entry := oldest.Value.(*cacheEntry)
		delete(c.elems, entry.path)
		c.curBytes -= entry.bytes
	}
}
