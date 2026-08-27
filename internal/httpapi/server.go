package httpapi

import (
	"net/http"

	"siphonic-roof-drainage-overflow-release/internal/app"
)

// Server wires the HTTP routes to the application service. It is the single
// runnable surface of the backend.
type Server struct {
	mux *http.ServeMux
	svc *app.Service
}

// NewServer builds the HTTP handler tree backed by the application service.
func NewServer(svc *app.Service) *Server {
	srv := &Server{mux: http.NewServeMux(), svc: svc}
	srv.mux.HandleFunc("GET /healthz", srv.handleHealth)
	srv.mux.HandleFunc("POST /v1/tasks", srv.handleCreateTask)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/lock", srv.handleLockTask)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/materials/operations", srv.handleMaterialOp)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/leases", srv.handleLease)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/leases/release", srv.handleLeaseRelease)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/welds/{weld}/generations/{generation}/stages", srv.handleWeldStage)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/welds/{weld}/inspections", srv.handleInspection)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/zones/{zone}/water-tests", srv.handleStartWaterTest)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/zones/{zone}/water-tests/advance", srv.handleAdvanceWaterTest)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/anomalies", srv.handleAnomaly)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/repairs", srv.handleRepair)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/reviews", srv.handleReview)
	srv.mux.HandleFunc("POST /v1/tasks/{id}/final-decisions", srv.handleFinalDecision)
	srv.mux.HandleFunc("GET /v1/tasks/{id}", srv.handleGetTask)
	srv.mux.HandleFunc("GET /v1/tasks/{id}/welds/{weld}/generations", srv.handleGetWeldGenerations)
	return srv
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
