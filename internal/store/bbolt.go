package store

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

var (
	bucketOperations = []byte("operations")
	bucketSnapshots  = []byte("snapshots")
)

// BoltStore is the transactional embedded-database implementation of Store,
// backed by bbolt. It is a single-file, crash-safe store with no external
// service dependency, so the same binary builds and runs on linux/amd64 and
// linux/arm64.
type BoltStore struct {
	db *bolt.DB
}

// Open opens (creating if necessary) the bbolt database at path and prepares
// the two buckets used by the service.
func Open(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketOperations); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSnapshots); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init bbolt buckets: %w", err)
	}
	return &BoltStore{db: db}, nil
}

// Begin starts a write transaction. bbolt serialises write transactions, so
// concurrent Begin calls are safe.
func (s *BoltStore) Begin() (Tx, error) {
	tx, err := s.db.Begin(true)
	if err != nil {
		return nil, fmt.Errorf("begin bbolt tx: %w", err)
	}
	return &boltTx{tx: tx}, nil
}

// Recover reads every committed snapshot and operation record into memory.
func (s *BoltStore) Recover() (Recovery, error) {
	rec := Recovery{
		Snapshots:  make(map[string][]byte),
		Operations: make(map[string]OperationRecord),
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(bucketSnapshots); b != nil {
			if err := b.ForEach(func(k, v []byte) error {
				rec.Snapshots[string(k)] = append([]byte(nil), v...)
				return nil
			}); err != nil {
				return err
			}
		}
		if b := tx.Bucket(bucketOperations); b != nil {
			if err := b.ForEach(func(k, v []byte) error {
				var op OperationRecord
				if err := json.Unmarshal(v, &op); err != nil {
					return fmt.Errorf("decode operation %q: %w", k, err)
				}
				rec.Operations[string(k)] = op
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Recovery{}, fmt.Errorf("recover bbolt: %w", err)
	}
	return rec, nil
}

// Close flushes and closes the underlying database.
func (s *BoltStore) Close() error { return s.db.Close() }

type boltTx struct {
	tx *bolt.Tx
}

func (t *boltTx) PutOperation(id domain.OperationID, digest string, result []byte) error {
	data, err := json.Marshal(OperationRecord{Digest: digest, Result: result})
	if err != nil {
		return err
	}
	return t.tx.Bucket(bucketOperations).Put([]byte(id), data)
}

func (t *boltTx) GetOperation(id domain.OperationID) (string, []byte, bool) {
	v := t.tx.Bucket(bucketOperations).Get([]byte(id))
	if v == nil {
		return "", nil, false
	}
	var op OperationRecord
	if err := json.Unmarshal(v, &op); err != nil {
		return "", nil, false
	}
	return op.Digest, op.Result, true
}

func (t *boltTx) PutSnapshot(taskID string, data []byte) error {
	return t.tx.Bucket(bucketSnapshots).Put([]byte(taskID), data)
}

func (t *boltTx) GetSnapshot(taskID string) ([]byte, bool) {
	v := t.tx.Bucket(bucketSnapshots).Get([]byte(taskID))
	if v == nil {
		return nil, false
	}
	return v, true
}

func (t *boltTx) Commit() error   { return t.tx.Commit() }
func (t *boltTx) Rollback() error { return t.tx.Rollback() }
