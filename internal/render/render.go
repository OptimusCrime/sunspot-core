// Package render provides HTTP response helpers.
package render

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/optimuscrime/sunspot-core/internal/resterr"
)

func JSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json")

	if re, ok := v.(resterr.Resterr); ok {
		if re.StatusCode >= 500 {
			slog.Error("request error", "err", re.Err, "path", r.URL.Path)
		}
		w.WriteHeader(re.StatusCode)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": re.Err.Error()})
		return
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		slog.Error("failed to encode response", "err", err, "path", r.URL.Path)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
