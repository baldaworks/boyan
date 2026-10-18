package canonicalsql

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// WalkProjectionManifests captures generation metadata and active references
// in one snapshot. A private spool bounds memory and releases canonical
// admission before callbacks. Callbacks must not mutate the canonical store.
func (o *Owner) WalkProjectionManifests(ctx context.Context, projectionID string,
	visit func(memory.ProjectionManifest, bool) error) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if visit == nil {
		return invalid("projection manifest visitor is required")
	}

	spool, err := os.CreateTemp("", "boyan-projection-manifests-*")
	if err != nil {
		return proofStoreFailure("create projection manifest spool", err)
	}
	defer os.Remove(spool.Name())
	defer spool.Close()

	if err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return spoolProjectionManifests(ctx, tx, projectionID, spool)
	}); err != nil {
		return err
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return proofStoreFailure("rewind projection manifest spool", err)
	}

	return visitProjectionSpool(ctx, spool, visit)
}

func visitProjectionSpool(
	ctx context.Context,
	spool io.Reader,
	visit func(memory.ProjectionManifest, bool) error,
) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		manifest, active, err := readProjectionVisit(spool)
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		if err := visit(manifest, active); err != nil {
			return err
		}
	}
}

func readProjectionVisit(spool io.Reader) (memory.ProjectionManifest, bool, error) {
	var header [5]byte
	if _, err := io.ReadFull(spool, header[:]); errors.Is(err, io.EOF) {
		return memory.ProjectionManifest{}, false, io.EOF
	} else if err != nil {
		return memory.ProjectionManifest{}, false, proofStoreFailure("read projection manifest spool", err)
	}

	size := binary.BigEndian.Uint32(header[:4])
	if size == 0 || size > MaxEnvelopeBytes || header[4] > 1 {
		return memory.ProjectionManifest{}, false, proofStoreFailure("invalid projection manifest spool", nil)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(spool, data); err != nil {
		return memory.ProjectionManifest{}, false, proofStoreFailure("read projection manifest spool", err)
	}

	var manifest memory.ProjectionManifest
	if err := Decode(ProjectionManifest, data, &manifest); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	return manifest, header[4] == 1, nil
}

func spoolProjectionManifests(ctx context.Context, tx *Tx, projectionID string, spool io.Writer) error {
	var after *Key
	for {
		rows, err := tx.Scan(
			ctx,
			Range{Family: ProjectionManifest, After: after, Limit: memory.MaxCanonicalReadRecords},
		)
		if err != nil {
			return err
		}

		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := spoolProjectionManifest(ctx, tx, projectionID, spool, row); err != nil {
				return err
			}
		}

		if len(rows) < memory.MaxCanonicalReadRecords {
			return nil
		}

		last := rows[len(rows)-1].Key
		after = &last
	}
}

func spoolProjectionManifest(ctx context.Context, tx *Tx, projectionID string, spool io.Writer, row Row) error {
	var manifest memory.ProjectionManifest
	if err := Decode(ProjectionManifest, row.Envelope, &manifest); err != nil {
		return err
	}

	if err := manifest.Validate(); err != nil {
		return err
	}

	if manifest.ProjectionID != projectionID {
		return nil
	}

	key, err := privacyKey(manifest.Scope, ProjectionActive, projectionID)
	if err != nil {
		return err
	}

	var active projectionActive
	if err := getPrivacyRecord(ctx, tx, key, ProjectionActive, &active); err != nil && !privacyMissing(err) {
		return err
	}

	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(row.Envelope)))

	if active.GenerationID == manifest.GenerationID {
		header[4] = 1
	}

	if _, err := spool.Write(header[:]); err != nil {
		return proofStoreFailure("write projection manifest spool", err)
	}

	if _, err := spool.Write(row.Envelope); err != nil {
		return proofStoreFailure("write projection manifest spool", err)
	}

	return nil
}
