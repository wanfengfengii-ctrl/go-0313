package app

import (
	"encoding/json"

	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/topology"
)

// jsonResult serialises a command result for storage as the idempotent
// operation record body.
func jsonResult(v any) ([]byte, error) {
	return json.Marshal(v)
}

// jsonUnmarshal decodes a stored result body back into its typed form.
func jsonUnmarshal(body []byte, v any) error {
	return json.Unmarshal(body, v)
}

// canonicalDigest returns the canonical request digest used for idempotency:
// the JSON serialisation of the request's semantic fields. Two requests with
// the same normalised content therefore share a digest.
func CanonicalDigest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// requireLocked rejects commands issued against a task that is not locked.
func requireLocked(st *TaskState) error {
	if st.Task.LockState != topology.LockStateLocked {
		return domain.NewError(domain.CodeInvalidArgument, "task is not locked")
	}
	return nil
}
