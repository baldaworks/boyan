package memory_test

import (
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestTemporalEligibilityUsesInclusiveLowerAndExclusiveUpperBounds(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	before, after := at.Add(-time.Nanosecond), at.Add(time.Nanosecond)

	for _, test := range []struct {
		name     string
		temporal m.Temporal
		want     bool
	}{
		{name: "absent_bounds", temporal: m.Temporal{ObservedAt: before}, want: true},
		{name: "observed_equal", temporal: m.Temporal{ObservedAt: at}, want: true},
		{name: "observed_future", temporal: m.Temporal{ObservedAt: after}},
		{name: "from_equal", temporal: m.Temporal{ObservedAt: before, ValidFrom: &at}, want: true},
		{name: "from_future", temporal: m.Temporal{ObservedAt: before, ValidFrom: &after}},
		{name: "until_equal", temporal: m.Temporal{ObservedAt: before, ValidUntil: &at}},
		{name: "until_future", temporal: m.Temporal{ObservedAt: before, ValidUntil: &after}, want: true},
		{name: "expiry_equal", temporal: m.Temporal{ObservedAt: before, ExpiresAt: &at}},
		{name: "expiry_future", temporal: m.Temporal{ObservedAt: before, ExpiresAt: &after}, want: true},
		{name: "event_past", temporal: m.Temporal{ObservedAt: before, EventAt: &before}, want: true},
		{name: "event_planned", temporal: m.Temporal{ObservedAt: before, EventAt: &after}, want: true},
		{name: "missing_observation", temporal: m.Temporal{}},
		{name: "intersection", temporal: m.Temporal{
			ObservedAt: at, ValidFrom: &before, ValidUntil: &after, ExpiresAt: &after,
		}, want: true},
		{name: "expired_intersection", temporal: m.Temporal{
			ObservedAt: at, ValidFrom: &before, ValidUntil: &after, ExpiresAt: &at,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := m.TemporalEligibleAt(test.temporal, at); got != test.want {
				t.Fatalf("eligibility = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTemporalEligibilityRequiresProcessingInstantAndComparesInstants(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	offset := at.In(time.FixedZone("offset", 6*60*60))
	temporal := m.Temporal{ObservedAt: offset, ValidFrom: &offset}

	if !m.TemporalEligibleAt(temporal, at) {
		t.Fatal("equivalent timestamp offsets changed eligibility")
	}

	if m.TemporalEligibleAt(temporal, time.Time{}) {
		t.Fatal("missing processing instant admitted a parent")
	}
}
