// Package store defines the transactional persistence boundary and its
// concrete embedded-database implementation. Every aggregate is committed
// atomically as a snapshot, and operation records provide the idempotency
// and restart-recovery source of truth. A process interruption before
// Commit leaves no partial state; after Commit a restart restores the same
// snapshot, idempotent response, lease deadline, retry sequence and final
// decision.
package store

import "siphonic-roof-drainage-overflow-release/internal/domain"

// OperationRecord is the canonical digest and serialised response stored for
// an idempotent command keyed by its OperationID.
type OperationRecord struct {
	Digest string `json:"digest"`
	Result []byte `json:"result"`
}

// Tx is a single atomic unit of work. Commit makes every write durable at
// once; Rollback discards the whole transaction.
type Tx interface {
	// PutOperation records an idempotent operation result keyed by the
	// OperationID against a canonical request digest.
	PutOperation(id domain.OperationID, digest string, result []byte) error
	// GetOperation returns a previously committed operation result and its
	// canonical digest, or false when the operation id is unknown.
	GetOperation(id domain.OperationID) (digest string, result []byte, ok bool)
	// PutSnapshot persists a full aggregate snapshot for a task.
	PutSnapshot(taskID string, data []byte) error
	// GetSnapshot returns a previously committed snapshot for a task.
	GetSnapshot(taskID string) (data []byte, ok bool)
	Commit() error
	Rollback() error
}

// Recovery is the committed-state view loaded on startup.
type Recovery struct {
	Snapshots  map[string][]byte
	Operations map[string]OperationRecord
}

// Store opens transactions and recovers committed state.
type Store interface {
	Begin() (Tx, error)
	// Recover returns every committed snapshot and operation record so the
	// service can rebuild its in-memory working set after a restart.
	Recover() (Recovery, error)
	Close() error
}
