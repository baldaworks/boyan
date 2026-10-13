package memory_test

import (
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestGroundingBoundsIntersectSelectedParentConstraints(t *testing.T) {
	t.Parallel()

	one := time.Date(2026, time.October, 13, 1, 0, 0, 0, time.UTC)
	two, three, four := one.Add(time.Hour), one.Add(2*time.Hour), one.Add(3*time.Hour)
	parents := []m.Temporal{
		{ValidFrom: &one, ValidUntil: &four, ExpiresAt: &three},
		{ValidFrom: &two, ValidUntil: &three, ExpiresAt: &four},
		{},
	}

	got, err := m.IntersectGroundingBounds(parents)
	if err != nil {
		t.Fatal(err)
	}

	want := m.GroundingBounds{ValidFrom: &two, ValidUntil: &three, ExpiresAt: &three}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intersection = %+v, want %+v", got, want)
	}

	*got.ValidFrom = four
	*got.ValidUntil = four

	*got.ExpiresAt = four
	if !parents[1].ValidFrom.Equal(two) || !parents[1].ValidUntil.Equal(three) ||
		!parents[0].ExpiresAt.Equal(three) {
		t.Fatal("changing output bounds changed immutable parent metadata")
	}
}

func TestGroundingBoundsKeepAbsentBoundsAndIgnoreDescriptiveEventTime(t *testing.T) {
	t.Parallel()

	event := time.Date(2026, time.October, 13, 1, 0, 0, 0, time.UTC)
	for _, parents := range [][]m.Temporal{nil, {{}}, {{EventAt: &event}, {ObservedAt: event}}} {
		got, err := m.IntersectGroundingBounds(parents)
		if err != nil || !reflect.DeepEqual(got, m.GroundingBounds{}) {
			t.Fatalf("absent constraints gained bounds: %+v, error=%v", got, err)
		}
	}
}

func TestGroundingBoundsRejectEmptyIntersections(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 1, 0, 0, 0, time.UTC)

	before, after := at.Add(-time.Nanosecond), at.Add(time.Nanosecond)
	for _, parents := range [][]m.Temporal{
		{{ValidFrom: &at}, {ValidUntil: &at}},
		{{ValidFrom: &at}, {ValidUntil: &before}},
		{{ValidFrom: &after}, {ExpiresAt: &at}},
		{{ValidFrom: &at}, {ExpiresAt: &at}},
	} {
		_, err := m.IntersectGroundingBounds(parents)
		if code, class, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived || class != m.ErrorClassPermanent {
			t.Fatalf("empty intersection accepted or misclassified: %v", err)
		}
	}
}
