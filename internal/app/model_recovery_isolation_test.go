package app_test

import (
	"path/filepath"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_MultiTaskRecoveryIsolation(t *testing.T) {
	cases := []struct {
		name  string
		count int
	}{
		{name: "single task and operation survive restart", count: 1},
		{name: "two recovered tasks remain independent", count: 2},
		{name: "many recovered tasks remain independent", count: 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			newService := func() *app.Service {
				t.Helper()
				svc, err := app.NewService(db, catalog.DemoSnapshot(), weld.PassThroughRegistry())
				if err != nil {
					t.Fatalf("restart service: %v", err)
				}
				return svc
			}

			svc := newService()
			ids := make([]domain.TaskID, tc.count)
			for i := range ids {
				ids[i] = domain.TaskID(tc.name + "-task-" + string(rune('A'+i)))
				req := app.CreateTaskRequest{TaskID: ids[i]}
				if _, err := svc.CreateTask(domain.OperationID("create-"+ids[i]), app.CanonicalDigest(req), req); err != nil {
					t.Fatalf("create %s: %v", ids[i], err)
				}
			}

			recovered := newService()
			for _, id := range ids {
				state, ok := recovered.GetTask(id)
				if !ok {
					t.Errorf("recovered task %s not found", id)
					continue
				}
				if state.Task.ID != id || state.Task.EventSeq != 1 || len(state.Events) != 1 || state.Events[0].Detail != string(id) {
					t.Errorf("recovered task %s contains another task's state: id=%s seq=%d events=%+v", id, state.Task.ID, state.Task.EventSeq, state.Events)
				}

				req := app.CreateTaskRequest{TaskID: id}
				replay, err := recovered.CreateTask(domain.OperationID("create-"+id), app.CanonicalDigest(req), req)
				if err != nil {
					t.Errorf("replay recovered operation for %s: %v", id, err)
				} else if replay.TaskID != id || replay.Version != 1 {
					t.Errorf("unexpected recovered operation result for %s: %+v", id, replay)
				}
			}

			first := ids[0]
			mutate := app.CreateTaskRequest{TaskID: first}
			if _, err := recovered.CreateTask(domain.OperationID("update-"+first), app.CanonicalDigest(mutate), mutate); err != nil {
				t.Fatalf("modify recovered task %s: %v", first, err)
			}
			for _, id := range ids[1:] {
				state, ok := recovered.GetTask(id)
				if !ok || state.Task.ID != id || state.Task.EventSeq != 1 || len(state.Events) != 1 {
					t.Errorf("modifying %s changed recovered task %s: ok=%v state=%+v", first, id, ok, state)
				}
			}

			restartedAgain := newService()
			for i, id := range ids {
				state, ok := restartedAgain.GetTask(id)
				wantSeq := int64(1)
				if i == 0 {
					wantSeq = 2
				}
				if !ok || state.Task.ID != id || state.Task.EventSeq != wantSeq || len(state.Events) != int(wantSeq) {
					t.Errorf("task %s not isolated after second restart: ok=%v want_seq=%d state=%+v", id, ok, wantSeq, state)
				}
			}
		})
	}
}
