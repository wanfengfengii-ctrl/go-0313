package weld

import "siphonic-roof-drainage-overflow-release/internal/domain"

// ScriptedAdapter is a deterministic DeviceAdapter whose behaviour is a fixed
// map from script key to outcome. It is the seam used by tests and the demo
// server to produce reproducible readings and classified failures without
// wall-clock time, randomness or external devices.
type ScriptedAdapter struct {
	Script map[string]ScriptOutcome
	// DefaultOutcome is returned for any key absent from Script.
	DefaultOutcome ScriptOutcome
}

// ScriptOutcome is a fixed result for a scripted device call: a successful
// reading, or a classified failure with its retry policy.
type ScriptOutcome struct {
	Reading int64
	Attempt DeviceAttempt
	Err     error
}

// Read returns the scripted outcome for key, or the default when key is not
// scripted.
func (a *ScriptedAdapter) Read(key string) (int64, DeviceAttempt, error) {
	out, ok := a.Script[key]
	if !ok {
		out = a.DefaultOutcome
	}
	return out.Reading, out.Attempt, out.Err
}

// ScriptedRegistry resolves resource ids to adapters. It backs the demo
// server and the concurrency / failure tests.
type ScriptedRegistry struct {
	Adapters map[domain.ResourceID]DeviceAdapter
}

// Adapter returns the adapter registered for resource, or false.
func (r *ScriptedRegistry) Adapter(resource domain.ResourceID) (DeviceAdapter, bool) {
	a, ok := r.Adapters[resource]
	return a, ok
}

// PassThroughAdapter succeeds for every key with a zero reading. It is the
// demo default wired in main so the server runs end-to-end without a device.
func PassThroughAdapter() *ScriptedAdapter {
	return &ScriptedAdapter{
		DefaultOutcome: ScriptOutcome{
			Reading: 0,
			Attempt: DeviceAttempt{ResultClass: ResultSuccess},
		},
	}
}

// PassThroughRegistry returns a registry that resolves every resource to a
// pass-through adapter.
func PassThroughRegistry(resources ...domain.ResourceID) *ScriptedRegistry {
	r := &ScriptedRegistry{Adapters: make(map[domain.ResourceID]DeviceAdapter, len(resources))}
	a := PassThroughAdapter()
	for _, id := range resources {
		r.Adapters[id] = a
	}
	return r
}
