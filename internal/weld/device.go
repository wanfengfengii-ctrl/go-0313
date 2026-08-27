package weld

import "siphonic-roof-drainage-overflow-release/internal/domain"

// DeviceAdapter is the seam between the weld trajectory recorder and physical
// devices. Concrete adapters are scripted in tests so results are
// deterministic: a fixed script key returns a fixed reading or a fixed
// failure, never a wall-clock or random value.
type DeviceAdapter interface {
	// Read executes the scripted call identified by key and returns either a
	// successful reading or a classified failure with its retry policy.
	Read(key string) (reading int64, attempt DeviceAttempt, err error)
}

// DeviceRegistry resolves device types to adapters by resource id. It is the
// point where a welder, clamp, planer or borescope channel is selected.
type DeviceRegistry interface {
	Adapter(resource domain.ResourceID) (DeviceAdapter, bool)
}
