package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/baldaworks/boyan/pkg/memory"
)

// The durable SQL pointer identifies the prior publication, not its wall time.
// It retains a candidate only when the complete physical tuple matches the
// supplied canonical scope, generation and watermark in this read snapshot.
func (store *Store) referencedPreviousBuildID(ctx context.Context,
	manifest memory.ProjectionManifest) (string, error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = snapshot.Close() }()

	var id string

	err = snapshot.tx.QueryRowContext(snapshot.ctx,
		"SELECT physical_build_id FROM lexical_previous WHERE scope_kind=? AND scope_key=?",
		manifest.Scope.Kind, manifest.Scope.Key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	physical, found, err := snapshot.committedLexical(manifest.Scope, manifest.GenerationID,
		manifest.Watermark, false, id)
	if err != nil || !found {
		return "", err
	}

	return physical.id, nil
}
