package app_test

import (
	"errors"
	"path/filepath"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/app"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/store"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_RestartRestoresTaskAndOperationLedger(t *testing.T) {
	originalRequest := app.CreateTaskRequest{TaskID: "committed-task"}
	originalDigest := app.CanonicalDigest(originalRequest)
	originalOperation := domain.OperationID("committed-operation")

	tests := []struct {
		name          string
		operationID   domain.OperationID
		request       app.CreateTaskRequest
		wantConflict  bool
		wantOriginal  bool
		wantTaskAdded bool
	}{
		{
			name:         "same digest replays original result",
			operationID:  originalOperation,
			request:      originalRequest,
			wantOriginal: true,
		},
		{
			name:         "different digest retains conflict",
			operationID:  originalOperation,
			request:      app.CreateTaskRequest{TaskID: "conflicting-task"},
			wantConflict: true,
		},
		{
			name:          "unused operation remains executable",
			operationID:   "unused-operation",
			request:       app.CreateTaskRequest{TaskID: "new-task"},
			wantTaskAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "service.db")
			beforeRestart, err := store.Open(databasePath)
			if err != nil {
				t.Fatalf("open store before restart: %v", err)
			}
			service, err := app.NewService(beforeRestart, catalog.DemoSnapshot(), weld.PassThroughRegistry("welder-1"))
			if err != nil {
				_ = beforeRestart.Close()
				t.Fatalf("create service before restart: %v", err)
			}
			originalResult, err := service.CreateTask(originalOperation, originalDigest, originalRequest)
			if err != nil {
				_ = beforeRestart.Close()
				t.Fatalf("commit original request: %v", err)
			}
			if err := beforeRestart.Close(); err != nil {
				t.Fatalf("close store for restart: %v", err)
			}

			afterRestart, err := store.Open(databasePath)
			if err != nil {
				t.Fatalf("reopen store after restart: %v", err)
			}
			t.Cleanup(func() { _ = afterRestart.Close() })
			restarted, err := app.NewService(afterRestart, catalog.DemoSnapshot(), weld.PassThroughRegistry("welder-1"))
			if err != nil {
				t.Fatalf("create service after restart: %v", err)
			}
			if recovered, ok := restarted.GetTask(originalRequest.TaskID); !ok || recovered.Task.EventSeq != originalResult.Version {
				t.Fatalf("committed task snapshot was not restored: ok=%v state=%+v", ok, recovered)
			}

			result, err := restarted.CreateTask(tt.operationID, app.CanonicalDigest(tt.request), tt.request)
			if tt.wantConflict {
				var stable *domain.StableError
				if !errors.As(err, &stable) || stable.Code != domain.CodeIdempotencyConflict {
					t.Fatalf("reused operation returned error %v, want %s", err, domain.CodeIdempotencyConflict)
				}
				if _, ok := restarted.GetTask(tt.request.TaskID); ok {
					t.Fatalf("conflicting replay created task %q", tt.request.TaskID)
				}
				return
			}
			if err != nil {
				t.Fatalf("request after restart failed: %v", err)
			}
			if tt.wantOriginal && *result != *originalResult {
				t.Fatalf("replay result = %+v, want original %+v", result, originalResult)
			}
			if tt.wantOriginal {
				state, _ := restarted.GetTask(originalRequest.TaskID)
				if state.Task.EventSeq != originalResult.Version {
					t.Fatalf("same-digest replay mutated event sequence to %d, want %d", state.Task.EventSeq, originalResult.Version)
				}
			}
			if tt.wantTaskAdded {
				if state, ok := restarted.GetTask(tt.request.TaskID); !ok || state.Task.EventSeq != result.Version {
					t.Fatalf("new operation did not create task normally: ok=%v state=%+v", ok, state)
				}
			}
		})
	}
}
