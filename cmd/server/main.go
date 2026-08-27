// Command server is the runnable entry point of the siphonic roof drainage
// overflow-release backend. It opens the transactional embedded database,
// recovers committed state, wires the rule snapshot and device registry, and
// serves the JSON command/query surface with a health check.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/httpapi"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func main() {
	addr := flag.String("addr", envOr("SIPHONIC_ADDR", ":8080"), "HTTP listen address")
	dataDir := flag.String("data-dir", envOr("SIPHONIC_DATA_DIR", "./data"), "persistence directory")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	dbPath := filepath.Join(*dataDir, "siphonic.db")
	boltStore, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer boltStore.Close()

	snapshot := catalog.DemoSnapshot()
	registry := weld.PassThroughRegistry("welder-1", "welder-2", "clamp-1", "clamp-2",
		"borescope-w1", "borescope-w2", "gauge-zone-A", "gauge-zone-B",
		"flow-zone-A", "flow-zone-B")

	svc, err := app.NewService(boltStore, snapshot, registry)
	if err != nil {
		log.Fatalf("build service: %v", err)
	}
	// A fixed demo reviewer directory so the review and final-decision flow is
	// usable end-to-end.
	svc.SetReviewerDirectory(map[string]app.ReviewerEntry{
		"reviewer-a": {Qualified: true, QualExpiry: 1 << 60},
		"reviewer-b": {Qualified: true, QualExpiry: 1 << 60},
		"reviewer-x": {Qualified: false, QualExpiry: 1 << 60},
	})

	srv := httpapi.NewServer(svc)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("siphonic roof drainage backend listening on %s (db %s)", *addr, dbPath)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited: %v", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
