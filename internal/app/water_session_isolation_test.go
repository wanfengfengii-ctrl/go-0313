package app

import (
	"reflect"
	"testing"

	"siphonic-roof-drainage-overflow-release/internal/arbitration"
	"siphonic-roof-drainage-overflow-release/internal/catalog"
	"siphonic-roof-drainage-overflow-release/internal/domain"
	"siphonic-roof-drainage-overflow-release/internal/lineage"
	"siphonic-roof-drainage-overflow-release/internal/weld"
)

func TestModel_WaterTestSessionsRemainIsolatedAcrossTasksAndRestart(t *testing.T) {
	svc, persistentStore := newTestService(t)
	devices := weld.PassThroughRegistry("gauge-zone-A", "flow-zone-A", "gauge-zone-B", "flow-zone-B")
	svc.devices = devices

	taskA, taskB := domain.TaskID("water-task-A"), domain.TaskID("water-task-B")
	zoneA, zoneB := domain.ZoneID("zone-A"), domain.ZoneID("zone-B")
	graphA := validGraph(domain.TaskID(zoneA), "drain-A", "segment-A", "outlet-A")
	graphB := validGraph(domain.TaskID(zoneB), "drain-B", "segment-B", "outlet-B")
	graphB.Segments[0].LengthMM = 2000
	lockTask(t, svc, taskA, graphA, nil)
	lockTask(t, svc, taskB, graphB, nil)

	for _, lease := range []struct {
		task domain.TaskID
		zone domain.ZoneID
	}{
		{task: taskA, zone: zoneA},
		{task: taskB, zone: zoneB},
	} {
		req := LeaseRequest{
			ResourceType: lineage.ResourceWaterZone,
			ResourceID:   domain.ResourceID("water-" + lease.zone),
			Holder:       lease.task,
			Start:        0,
			End:          10000,
		}
		if _, err := svc.AcquireLease(lease.task, domain.OperationID("lease-"+lease.task), CanonicalDigest(req), req); err != nil {
			t.Fatalf("acquire lease for %s: %v", lease.task, err)
		}
	}

	volumeA, err := arbitration.PipeVolumeMM3(110, 1000)
	if err != nil {
		t.Fatal(err)
	}
	volumeB, err := arbitration.PipeVolumeMM3(110, 2000)
	if err != nil {
		t.Fatal(err)
	}

	type expectedSession struct {
		task    domain.TaskID
		zone    domain.ZoneID
		session arbitration.WaterTestSession
	}
	type testCase struct {
		name string
		act  func() error
		want []expectedSession
	}

	fillA := arbitration.WaterReading{Kind: "WATER_LEVEL", Value: 501, LogicalTime: 110, Valid: true}
	flowA := arbitration.WaterReading{Kind: "FLOW", Value: volumeA / 1000, LogicalTime: 1200, Valid: true}
	fillB := arbitration.WaterReading{Kind: "WATER_LEVEL", Value: 702, LogicalTime: 210, Valid: true}

	cases := []testCase{
		{
			name: "starting both tasks preserves each identity and volume",
			act: func() error {
				if _, err := svc.StartWaterTest(taskA, "start-A", "start-A", zoneA, 100); err != nil {
					return err
				}
				_, err := svc.StartWaterTest(taskB, "start-B", "start-B", zoneB, 200)
				return err
			},
			want: []expectedSession{
				{task: taskA, zone: zoneA, session: arbitration.WaterTestSession{Task: taskA, Zone: zoneA, Phase: arbitration.WaterPhaseFill, VolumeMM3: volumeA, FillTime: 100}},
				{task: taskB, zone: zoneB, session: arbitration.WaterTestSession{Task: taskB, Zone: zoneB, Phase: arbitration.WaterPhaseFill, VolumeMM3: volumeB, FillTime: 200}},
			},
		},
		{
			name: "advancing task A does not change task B",
			act: func() error {
				req := WaterTestRequest{Zone: zoneA, Phase: arbitration.WaterPhaseFill, Value: fillA.Value, LogicalTime: fillA.LogicalTime}
				_, err := svc.AdvanceWaterTest(taskA, "fill-A", CanonicalDigest(req), req)
				return err
			},
			want: []expectedSession{
				{task: taskA, zone: zoneA, session: arbitration.WaterTestSession{Task: taskA, Zone: zoneA, Phase: arbitration.WaterPhaseHold, VolumeMM3: volumeA, FillTime: 100, Readings: []arbitration.WaterReading{fillA}}},
				{task: taskB, zone: zoneB, session: arbitration.WaterTestSession{Task: taskB, Zone: zoneB, Phase: arbitration.WaterPhaseFill, VolumeMM3: volumeB, FillTime: 200}},
			},
		},
		{
			name: "restart recovers the two sessions independently",
			act: func() error {
				restarted, err := NewService(persistentStore, catalog.DemoSnapshot(), devices)
				if err == nil {
					svc = restarted
				}
				return err
			},
			want: []expectedSession{
				{task: taskA, zone: zoneA, session: arbitration.WaterTestSession{Task: taskA, Zone: zoneA, Phase: arbitration.WaterPhaseHold, VolumeMM3: volumeA, FillTime: 100, Readings: []arbitration.WaterReading{fillA}}},
				{task: taskB, zone: zoneB, session: arbitration.WaterTestSession{Task: taskB, Zone: zoneB, Phase: arbitration.WaterPhaseFill, VolumeMM3: volumeB, FillTime: 200}},
			},
		},
		{
			name: "advancing task B does not change task A",
			act: func() error {
				req := WaterTestRequest{Zone: zoneB, Phase: arbitration.WaterPhaseFill, Value: fillB.Value, LogicalTime: fillB.LogicalTime}
				_, err := svc.AdvanceWaterTest(taskB, "fill-B", CanonicalDigest(req), req)
				return err
			},
			want: []expectedSession{
				{task: taskA, zone: zoneA, session: arbitration.WaterTestSession{Task: taskA, Zone: zoneA, Phase: arbitration.WaterPhaseHold, VolumeMM3: volumeA, FillTime: 100, Readings: []arbitration.WaterReading{fillA}}},
				{task: taskB, zone: zoneB, session: arbitration.WaterTestSession{Task: taskB, Zone: zoneB, Phase: arbitration.WaterPhaseHold, VolumeMM3: volumeB, FillTime: 200, Readings: []arbitration.WaterReading{fillB}}},
			},
		},
		{
			name: "completing task A leaves task B barrier closed",
			act: func() error {
				requests := []WaterTestRequest{
					{Zone: zoneA, Phase: arbitration.WaterPhaseHold, LogicalTime: 1100},
					{Zone: zoneA, Phase: arbitration.WaterPhaseDrain, LogicalTime: 1200, DrainDurationMS: 1000},
					{Zone: zoneA, Phase: arbitration.WaterPhaseEmpty, LogicalTime: 1300},
				}
				for i, req := range requests {
					if _, err := svc.AdvanceWaterTest(taskA, domain.OperationID("finish-A-"+string(rune('0'+i))), CanonicalDigest(req), req); err != nil {
						return err
					}
				}
				return nil
			},
			want: []expectedSession{
				{task: taskA, zone: zoneA, session: arbitration.WaterTestSession{Task: taskA, Zone: zoneA, Phase: arbitration.WaterPhaseComplete, VolumeMM3: volumeA, FillTime: 100, Readings: []arbitration.WaterReading{fillA, flowA}, DrainedOK: true, BarrierOpen: true}},
				{task: taskB, zone: zoneB, session: arbitration.WaterTestSession{Task: taskB, Zone: zoneB, Phase: arbitration.WaterPhaseHold, VolumeMM3: volumeB, FillTime: 200, Readings: []arbitration.WaterReading{fillB}}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.act(); err != nil {
				t.Fatalf("operation failed: %v", err)
			}
			for _, want := range tc.want {
				state, ok := svc.GetTask(want.task)
				if !ok {
					t.Fatalf("task %s not found", want.task)
				}
				got, ok := state.WaterTests[want.zone]
				if !ok {
					t.Fatalf("task %s has no session for %s", want.task, want.zone)
				}
				if !reflect.DeepEqual(*got, want.session) {
					t.Fatalf("task %s session changed across task boundary:\n got: %+v\nwant: %+v", want.task, *got, want.session)
				}
			}
		})
	}
}
