package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.ProjectionRetentionFloorWriter = (*Owner)(nil)

type projectionRetention struct {
	Floor uint64 `json:"floor"`
}

// LoadProjectionRetentionFloor returns zero when no retention has occurred.
func (o *Owner) LoadProjectionRetentionFloor(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (uint64, error) {
	if err := validateProjectionLookup(ctx, scope, projectionID); err != nil {
		return 0, err
	}

	key, err := privacyKey(scope, ProjectionRetention, projectionID)
	if err != nil {
		return 0, err
	}

	var retention projectionRetention

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		err := getPrivacyRecord(ctx, tx, key, ProjectionRetention, &retention)
		if privacyMissing(err) {
			return nil
		}

		return err
	})

	return retention.Floor, err
}

// SetProjectionRetentionFloor atomically records a monotone maintenance cursor.
func (o *Owner) SetProjectionRetentionFloor(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
	floor uint64,
) error {
	if err := validateProjectionLookup(ctx, scope, projectionID); err != nil {
		return err
	}

	key, err := privacyKey(scope, ProjectionRetention, projectionID)
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		var current projectionRetention

		err := getPrivacyRecord(ctx, tx, key, ProjectionRetention, &current)
		if err != nil && !privacyMissing(err) {
			return err
		}

		if err == nil && floor < current.Floor {
			return memory.PermanentError(memory.CodeConflict, "projection retention floor moved backwards", nil)
		}

		return putPrivacyRecord(ctx, tx, key, ProjectionRetention, projectionRetention{Floor: floor})
	})
}
