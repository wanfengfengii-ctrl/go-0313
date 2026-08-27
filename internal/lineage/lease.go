package lineage

import "siphonic-roof-drainage-overflow-release/internal/domain"

// ResourceType is one of the six mutex resources the domain leases: welder,
// clamp, planer, work zone, borescope channel and water-test zone.
type ResourceType string

const (
	ResourceWelder    ResourceType = "WELDER"
	ResourceClamp     ResourceType = "CLAMP"
	ResourcePlaner    ResourceType = "PLANER"
	ResourceWorkZone  ResourceType = "WORK_ZONE"
	ResourceBorescope ResourceType = "BORESCOPE"
	ResourceWaterZone ResourceType = "WATER_ZONE"
)

// Lease is a time-limited, mutually exclusive hold on a resource. Time is
// expressed in logical clock units so leases are deterministic and survive
// restart without wall-clock dependence.
type Lease struct {
	ResourceType ResourceType
	ResourceID   domain.ResourceID
	Holder       domain.TaskID
	Weld         domain.WeldID
	Generation   domain.Generation
	Start        int64
	End          int64
	Released     bool
	Reason       string
}

// Overlaps reports whether two leases on the same resource have overlapping
// effective time intervals. Two effective intervals on one resource must
// never overlap.
func (l Lease) Overlaps(o Lease) bool {
	if l.ResourceType != o.ResourceType || l.ResourceID != o.ResourceID {
		return false
	}
	if l.Released || o.Released {
		return false
	}
	return l.Start < o.End && o.Start < l.End
}

// Expired reports whether the lease has elapsed at the given logical time.
func (l Lease) Expired(now int64) bool {
	return !l.Released && now >= l.End
}
