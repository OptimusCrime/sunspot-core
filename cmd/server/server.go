package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/optimuscrime/sunspot-core/internal/api"
	"github.com/optimuscrime/sunspot-core/internal/dataset"
	"github.com/optimuscrime/sunspot-core/internal/shadow"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		slog.Error("DATA_DIR environment variable is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("loading datasets", "dir", dataDir)
	datasets, err := dataset.Discover(dataDir, dataset.LoadOptions{LoadPixels: false})
	if err != nil {
		slog.Error("discover datasets", "err", err)
		os.Exit(1)
	}
	if len(datasets) == 0 {
		slog.Error("no datasets found", "dir", dataDir)
		os.Exit(1)
	}

	printDatasets(datasets)

	slog.Info("building tile indices")
	domIndex, dtmIndex, err := buildIndices(datasets)
	if err != nil {
		slog.Error("build indices", "err", err)
		os.Exit(1)
	}

	server := api.NewServer(domIndex, dtmIndex, shadow.DefaultOptions())

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
	}))

	server.Routes(r)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting server", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}

func buildIndices(datasets []*dataset.Dataset) (*shadow.TileIndex, *shadow.TileIndex, error) {
	domEntries := dataset.MergeTiles(datasets, dataset.DOM)
	dtmEntries := dataset.MergeTiles(datasets, dataset.DTM)

	domIndex, domStats, err := shadow.NewTileIndex(domEntries)
	if err != nil {
		return nil, nil, fmt.Errorf("DOM index: %w", err)
	}

	dtmIndex, dtmStats, err := shadow.NewTileIndex(dtmEntries)
	if err != nil {
		return nil, nil, fmt.Errorf("DTM index: %w", err)
	}

	slog.Info("tile indices built", "dom_cells", domStats.TotalCells, "dtm_cells", dtmStats.TotalCells)
	logSourceBreakdown("DOM", domStats, datasets)
	logSourceBreakdown("DTM", dtmStats, datasets)

	return domIndex, dtmIndex, nil
}

func printDatasets(datasets []*dataset.Dataset) {
	sorted := sortedByPriority(datasets)
	slog.Info("discovered datasets", "count", len(sorted))
	for i, ds := range sorted {
		slog.Info("dataset", "priority", i+1, "name", ds.Name, "dom_tiles", len(ds.DOMTiles()), "dtm_tiles", len(ds.DTMTiles()))
	}
}

func logSourceBreakdown(label string, stats *shadow.IndexStats, datasets []*dataset.Dataset) {
	for _, ds := range sortedByPriority(datasets) {
		n := stats.TilesBySource[ds.Root]
		if n > 0 {
			slog.Info("index source", "index", label, "dataset", ds.Name, "cells", n)
		}
	}
}

func sortedByPriority(datasets []*dataset.Dataset) []*dataset.Dataset {
	sorted := make([]*dataset.Dataset, len(datasets))
	copy(sorted, datasets)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name > sorted[j].Name
	})
	return sorted
}
