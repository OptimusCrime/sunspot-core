# sunspot-core

A Go REST API that calculates sun and shade conditions for a geographic point (latitude/longitude) throughout a given day. It uses real-world elevation data in GeoTIFF format to determine whether buildings or terrain block the sun's rays at each minute from sunrise to sunset.

## How it works

At startup the server discovers one or more elevation datasets from the configured data directory. Each dataset provides two types of raster tiles:

- **DOM** (Digital Surface Model) — elevation including buildings, trees, and other structures above ground.
- **DTM** (Digital Terrain Model) — bare-earth elevation without above-ground features.

For each query the algorithm:

1. Converts the query lat/lon to UTM coordinates.
2. Looks up the ground height at that point from the DTM, then adds a standing eye height (1.5 m by default).
3. Iterates every minute between sunrise and sunset, computing the sun's azimuth and altitude via SunCalc.
4. For each minute where the sun is above a minimum altitude threshold (5° by default), casts a ray from the observer toward the sun through the DOM tiles using an adaptive step size.
5. If the DOM surface height exceeds the ray height at any point, the observer is in shade and the blocking location is recorded.

The response groups consecutive same-state minutes into segments and returns total sun/shade minutes alongside sunrise and sunset times.

## Endpoints

### `POST /shadow`

**Request body:**
```json
{
  "lat": 59.925,
  "lon": 10.759,
  "date": "2024-06-15"
}
```

**Response:**
```json
{
  "sunrise": "2024-06-15T03:51:00Z",
  "sunset": "2024-06-15T21:43:00Z",
  "minutes_in_sun": 412,
  "minutes_in_shade": 280,
  "segments": [
    {
      "from": "2024-06-15T03:51:00Z",
      "to": "2024-06-15T07:10:00Z",
      "state": "shade",
      "minutes": 200,
      "sun_azimuth": 48.3,
      "sun_altitude": 5.1,
      "blocked_by": {
        "lat": 59.924,
        "lon": 10.761,
        "distance_m": 38.5,
        "surface_height_m": 22.4
      }
    }
  ]
}
```

**Status codes:**
- `200` — success
- `400` — invalid request (bad coordinates or date format)
- `404` — the query point falls outside all loaded elevation tiles
- `500` — internal error

## Configuration

The server is configured entirely through environment variables:

| Variable   | Default | Description                                              |
|------------|---------|----------------------------------------------------------|
| `PORT`     | `8080`  | TCP port the HTTP server listens on                      |
| `DATA_DIR` | —       | **Required.** Path to the directory containing datasets  |

## Data directory layout

```
<DATA_DIR>/
  <dataset name>/
    data/
      dom/   ← GeoTIFF Digital Surface Model tiles (*.tif)
      dtm/   ← GeoTIFF Digital Terrain Model tiles (*.tif)
    metadata/
```

Each immediate subdirectory of `DATA_DIR` is treated as one named dataset. When multiple datasets cover the same area, the lexicographically latest dataset name wins (so year-suffixed names like `Oslo 2014` and `Oslo 2019` resolve correctly).

## Running

```bash
DATA_DIR=/path/to/datasets PORT=8080 go run ./cmd/server
```

## Project structure

```
cmd/server/        — server entry point
internal/
  api/             — HTTP handlers
  dataset/         — dataset discovery and tile loading
  geotiff/         — GeoTIFF file reader
  render/          — JSON response helper
  resterr/         — typed HTTP error
  shadow/          — shadow calculation and tile index
  sosi/            — Norwegian SOSI metadata parser
  sun/             — sun position calculations
  utm/             — UTM ↔ WGS84 coordinate conversion
```
