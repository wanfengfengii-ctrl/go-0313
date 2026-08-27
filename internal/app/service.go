package app

import (
	"encoding/json"
	"fmt"
	"sync"

	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

// Service is the application service. It is safe for concurrent use: every
// command is serialised under a mutex, applied to the in-memory working set,
// and persisted as a single atomic transaction before the mutex is released.
type Service struct {
	mu       sync.Mutex
	store    store.Store
	snapshot *catalog.RuleSnapshot
	devices  weld.DeviceRegistry
	reviewer map[string]reviewerRec

	tasks map[domain.TaskID]*TaskState
	ops   map[domain.OperationID]opRecord

	// FailBeforeCommit is a test hook invoked immediately before the store
	// transaction commits. Returning an error simulates a crash at the commit
	// boundary so rollback recovery can be exercised deterministically.
	FailBeforeCommit func() error
}

// reviewerRec holds the reviewer directory entry used by review validation.
type reviewerRec struct {
	Qualified  bool
	QualExpiry int64
}

type opRecord struct {
	Digest string
	Result []byte
}

// opKey scopes an operation id to the task it targets. Operation-Id is the
// idempotency key for replay protection within a single task: replaying the
// same Operation-Id against the same task must return the cached result, but
// the same Operation-Id used against a different task is a distinct logical
// operation (each task manages its own material cuts and port bindings) and
// must execute rather than cross-replay another task's stored result. The
// composite key keeps each (task, operation) record independent in both the
// in-memory cache and the durable operation store.
func opKey(taskID domain.TaskID, opID domain.OperationID) domain.OperationID {
	if opID == "" {
		return ""
	}
	return domain.OperationID(string(taskID) + "\x00" + string(opID))
}

// NewService builds a Service, recovering any committed state from the store
// so a restart resumes with identical snapshots, leases, idempotent responses
// and final decisions.
func NewService(s store.Store, snapshot *catalog.RuleSnapshot, devices weld.DeviceRegistry) (*Service, error) {
	svc := &Service{
		store:    s,
		snapshot: snapshot,
		devices:  devices,
		tasks:    make(map[domain.TaskID]*TaskState),
		ops:      make(map[domain.OperationID]opRecord),
	}
	if s == nil {
		return nil, domain.NewError(domain.CodeInternal, "store is nil")
	}
	rec, err := s.Recover()
	if err != nil {
		return nil, err
	}
	for id, data := range rec.Snapshots {
		var st TaskState
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, fmt.Errorf("recover task %s: %w", id, err)
		}
		svc.tasks[domain.TaskID(id)] = &st
	}
	for id, op := range rec.Operations {
		svc.ops[domain.OperationID(id)] = opRecord{Digest: op.Digest, Result: op.Result}
	}
	return svc, nil
}

// SetReviewerDirectory installs the reviewer qualification directory. It is
// used by the review and final-decision flows.
func (s *Service) SetReviewerDirectory(dir map[string]ReviewerEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reviewer = make(map[string]reviewerRec, len(dir))
	for id, e := range dir {
		s.reviewer[id] = reviewerRec{Qualified: e.Qualified, QualExpiry: e.QualExpiry}
	}
}

// ReviewerEntry is a reviewer directory entry.
type ReviewerEntry struct {
	Qualified  bool
	QualExpiry int64
}

// runCommand is the single transaction boundary for every write command. It
// checks idempotency, loads or creates the task, applies fn, and persists the
// whole snapshot plus the operation record atomically.
func (s *Service) runCommand(taskID domain.TaskID, opID domain.OperationID, digest string, resultJSON func(*TaskState) ([]byte, error)) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := opKey(taskID, opID)
	if key != "" {
		if prev, ok := s.ops[key]; ok {
			if prev.Digest != digest {
				return nil, domain.NewError(domain.CodeIdempotencyConflict, "operation_id reused with different content")
			}
			return prev.Result, nil
		}
	}

	st := s.tasks[taskID]
	if st == nil {
		st = NewTaskState(taskID)
	}

	body, err := resultJSON(st)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(st)
	if err != nil {
		return nil, domain.NewError(domain.CodeInternal, "serialise snapshot: "+err.Error())
	}

	if err := s.persist(taskID, key, digest, body, data); err != nil {
		// Reload committed state so a failed commit leaves no in-memory
		// partial side effect either.
		if _, ok := s.ops[key]; !ok {
			// The operation was not committed; drop any in-memory change by
			// re-reading the store snapshot for this task.
			s.tasks[taskID] = s.reload(taskID)
		}
		return nil, err
	}
	s.tasks[taskID] = st
	if key != "" {
		s.ops[key] = opRecord{Digest: digest, Result: body}
	}
	return body, nil
}

func (s *Service) persist(taskID domain.TaskID, opKey domain.OperationID, digest string, result, snapshot []byte) error {
	tx, err := s.store.Begin()
	if err != nil {
		return domain.NewError(domain.CodeInternal, "begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	if err := tx.PutSnapshot(string(taskID), snapshot); err != nil {
		return domain.NewError(domain.CodeInternal, "write snapshot: "+err.Error())
	}
	if opKey != "" {
		if err := tx.PutOperation(opKey, digest, result); err != nil {
			return domain.NewError(domain.CodeInternal, "write operation: "+err.Error())
		}
	}
	if s.FailBeforeCommit != nil {
		if err := s.FailBeforeCommit(); err != nil {
			return domain.NewError(domain.CodeInternal, "injected commit failure: "+err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.NewError(domain.CodeInternal, "commit: "+err.Error())
	}
	return nil
}

func (s *Service) reload(taskID domain.TaskID) *TaskState {
	rec, err := s.store.Recover()
	if err != nil {
		return nil
	}
	if data, ok := rec.Snapshots[string(taskID)]; ok {
		var st TaskState
		if err := json.Unmarshal(data, &st); err == nil {
			return &st
		}
	}
	return nil
}

// GetTask returns a deep, serialised copy of the task state for query
// endpoints. It returns false when the task is unknown.
func (s *Service) GetTask(taskID domain.TaskID) (*TaskState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	var cp TaskState
	data, _ := json.Marshal(st)
	_ = json.Unmarshal(data, &cp)
	return &cp, true
}
