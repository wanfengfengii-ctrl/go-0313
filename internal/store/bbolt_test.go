package store

import (
	"path/filepath"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/domain"
)

func TestSnapshotPersistAndRecover(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutSnapshot("task-1", []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutOperation("op-1", "digest", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rec, err := s.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Snapshots["task-1"]) != `{"x":1}` {
		t.Fatalf("snapshot not recovered: %q", rec.Snapshots["task-1"])
	}
	if rec.Operations["op-1"].Digest != "digest" {
		t.Fatalf("operation not recovered: %+v", rec.Operations["op-1"])
	}
}

func TestRollbackLeavesNoState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, _ := s.Begin()
	_ = tx.PutSnapshot("task-1", []byte(`{"x":1}`))
	_ = tx.PutOperation("op-1", "digest", []byte(`{}`))
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	rec, _ := s.Recover()
	if _, ok := rec.Snapshots["task-1"]; ok {
		t.Fatal("rolled-back snapshot must not be recovered")
	}
	if _, ok := rec.Operations["op-1"]; ok {
		t.Fatal("rolled-back operation must not be recovered")
	}
}

func TestGetOperationInTx(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "test.db"))
	defer s.Close()

	tx, _ := s.Begin()
	_ = tx.PutOperation(domain.OperationID("op-1"), "d", []byte("r"))
	_ = tx.Commit()

	tx2, _ := s.Begin()
	defer tx2.Rollback()
	d, r, ok := tx2.GetOperation("op-1")
	if !ok || d != "d" || string(r) != "r" {
		t.Fatalf("operation read failed: ok=%v d=%q r=%q", ok, d, r)
	}
}
