// Package httpapi exposes the JSON command and query endpoints, the stable
// error protocol, Operation-Id idempotency control, transaction boundaries,
// health check and restart-recovery entry point.
package httpapi

import (
	"encoding/json"
	"net/http"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

// ErrorResponse is the stable JSON error envelope returned by every write
// endpoint. Reasons are ordered per the domain protocol and the original
// transaction version is echoed so retries can compare against it.
type ErrorResponse struct {
	Code    domain.ErrorCode `json:"code"`
	Message string           `json:"message"`
	Reasons []string         `json:"reasons"`
	Version int64            `json:"version"`
}

// writeError serialises a domain StableError as the documented JSON protocol.
func writeError(w http.ResponseWriter, status int, err *domain.StableError) {
	resp := ErrorResponse{Code: domain.CodeInternal, Message: "internal error"}
	if err != nil {
		resp = ErrorResponse{
			Code:    err.Code,
			Message: err.Error(),
			Reasons: err.Reasons,
			Version: err.Version,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// statusForError maps a stable error code to an HTTP status.
func statusForError(code domain.ErrorCode) int {
	switch code {
	case domain.CodeOK:
		return http.StatusOK
	case domain.CodeNotFound:
		return http.StatusNotFound
	case domain.CodeIdempotencyConflict, domain.CodeFinalConflict:
		return http.StatusConflict
	case domain.CodeInvalidArgument, domain.CodeStaleSummary,
		domain.CodeDuplicatePort, domain.CodeCycle, domain.CodeDisconnected,
		domain.CodeIllegalDiameter, domain.CodeDegenerate, domain.CodeOverflow,
		domain.CodeDivideByZero, domain.CodeLineageBreach, domain.CodePortInUse,
		domain.CodeLeaseConflict, domain.CodeStageOutOfOrder,
		domain.CodeDeviceFailure:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
