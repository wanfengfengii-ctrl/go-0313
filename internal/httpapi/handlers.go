package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

// decode reads the JSON request body into v, writing a 400 on failure.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.CodeInvalidArgument, "malformed JSON body"))
		return false
	}
	return true
}

// operationID returns the Operation-Id header required by every write
// endpoint.
func operationID(r *http.Request) domain.OperationID {
	return domain.OperationID(r.Header.Get("Operation-Id"))
}

func requireOpID(w http.ResponseWriter, r *http.Request) (domain.OperationID, bool) {
	op := operationID(r)
	if op == "" {
		writeError(w, http.StatusBadRequest, domain.NewError(domain.CodeInvalidArgument, "Operation-Id header is required"))
		return "", false
	}
	return op, true
}

// respond writes a successful JSON response.
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondErr writes a domain error with the mapped HTTP status.
func respondErr(w http.ResponseWriter, err error) {
	se, ok := err.(*domain.StableError)
	if !ok {
		se = domain.NewError(domain.CodeInternal, err.Error())
	}
	writeError(w, statusForError(se.Code), se)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req app.CreateTaskRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.CreateTask(op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusCreated, res)
}

func (s *Server) handleLockTask(w http.ResponseWriter, r *http.Request) {
	var req app.LockTaskRequest
	if !decode(w, r, &req) {
		return
	}
	req.TaskID = domain.TaskID(r.PathValue("id"))
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.LockTask(op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleMaterialOp(w http.ResponseWriter, r *http.Request) {
	var req app.MaterialOpRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.MaterialOp(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	var req app.LeaseRequest
	if !decode(w, r, &req) {
		return
	}
	req.Holder = domain.TaskID(r.PathValue("id"))
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.AcquireLease(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleLeaseRelease(w http.ResponseWriter, r *http.Request) {
	var req app.LeaseRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.ReleaseLease(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleWeldStage(w http.ResponseWriter, r *http.Request) {
	var req app.WeldStageRequest
	if !decode(w, r, &req) {
		return
	}
	req.Weld = domain.WeldID(r.PathValue("weld"))
	req.Generation = domain.Generation(parseInt(r.PathValue("generation")))
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.SubmitWeldStage(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleInspection(w http.ResponseWriter, r *http.Request) {
	var req app.InspectionRequest
	if !decode(w, r, &req) {
		return
	}
	req.Weld = domain.WeldID(r.PathValue("weld"))
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.SubmitInspection(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleStartWaterTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LogicalTime int64 `json:"logical_time"`
	}
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.StartWaterTest(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), domain.ZoneID(r.PathValue("zone")), req.LogicalTime)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleAdvanceWaterTest(w http.ResponseWriter, r *http.Request) {
	var req app.WaterTestRequest
	if !decode(w, r, &req) {
		return
	}
	req.Zone = domain.ZoneID(r.PathValue("zone"))
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.AdvanceWaterTest(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleAnomaly(w http.ResponseWriter, r *http.Request) {
	var req app.AnomalyRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.RegisterAnomaly(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleRepair(w http.ResponseWriter, r *http.Request) {
	var req app.RepairRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.Repair(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req app.ReviewRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.SubmitReview(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleFinalDecision(w http.ResponseWriter, r *http.Request) {
	var req app.FinalDecisionRequest
	if !decode(w, r, &req) {
		return
	}
	op, ok := requireOpID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.FinalDecision(domain.TaskID(r.PathValue("id")), op, app.CanonicalDigest(req), req)
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, res)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	st, ok := s.svc.GetTask(domain.TaskID(r.PathValue("id")))
	if !ok {
		writeError(w, http.StatusNotFound, domain.NewError(domain.CodeNotFound, "task not found"))
		return
	}
	respond(w, http.StatusOK, st)
}

func (s *Server) handleGetWeldGenerations(w http.ResponseWriter, r *http.Request) {
	st, ok := s.svc.GetTask(domain.TaskID(r.PathValue("id")))
	if !ok {
		writeError(w, http.StatusNotFound, domain.NewError(domain.CodeNotFound, "task not found"))
		return
	}
	weldID := domain.WeldID(r.PathValue("weld"))
	respond(w, http.StatusOK, map[string]any{
		"weld":        weldID,
		"current":     st.CurrentGen[weldID],
		"generations": st.Welds[weldID],
		"reviews":     st.Reviews,
		"final":       st.Final,
		"repair_sets": st.RepairSets,
		"anomalies":   st.Anomalies,
		"water_tests": st.WaterTests,
		"attempts":    st.Attempts,
		"events":      st.Events,
	})
}

// parseInt parses a path segment as an int64, returning zero on failure. It
// is only used for the generation path segment, which is always numeric.
func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// compile-time assertion that the weld stage constants remain wired.
var _ = weld.StageTrimming
