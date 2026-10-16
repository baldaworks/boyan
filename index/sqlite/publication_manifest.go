package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/baldaworks/boyan/pkg/memory"
)

type physicalLexicalManifest struct {
	id       string
	manifest memory.ProjectionManifest
}

const committedLexicalQuery = `SELECT b.physical_build_id,b.generation_id,
 CASE WHEN typeof(b.watermark)='blob' AND length(b.watermark)=8 THEN b.watermark END,
 b.record_count,b.source_count,b.conversation_count,
 (SELECT count(*) FROM lexical_record r WHERE r.physical_build_id=b.physical_build_id),
 (SELECT count(*) FROM lexical_record r JOIN lexical_fts f ON f.rowid=r.rowid
 WHERE r.physical_build_id=b.physical_build_id),
 (SELECT count(*) FROM lexical_source s JOIN lexical_record r ON r.rowid=s.record_rowid
 WHERE r.physical_build_id=b.physical_build_id),
 (SELECT count(*) FROM lexical_conversation c JOIN lexical_record r ON r.rowid=c.record_rowid
 WHERE r.physical_build_id=b.physical_build_id)
 FROM lexical_build b `

// committedLexical validates the complete durable marker in the same snapshot
// as its rows. A missing or revoked marker is a rebuild condition; a forged
// complete marker with missing rows is storage corruption.
func (snapshot *readSnapshot) committedLexical(scope memory.Scope, generation string,
	watermark uint64, active bool, preferred ...string) (physicalLexicalManifest, bool, error) {
	query := committedLexicalQuery
	if active {
		query += ` JOIN lexical_active a USING(physical_build_id,scope_kind,scope_key) `
	}

	query += ` WHERE b.scope_kind=? AND b.scope_key=? AND b.generation_id=? AND b.watermark=?
 AND b.projection_id='sqlite' AND b.state='committed' AND b.revoked=0
 AND b.schema_version=1 AND b.preprocessing_version=1`
	args := []any{scope.Kind, scope.Key, generation, encodeUint64(watermark)}

	if len(preferred) > 0 {
		query += ` AND b.physical_build_id=?`

		args = append(args, preferred[0])
	}

	query += ` ORDER BY CASE
 WHEN EXISTS(SELECT 1 FROM lexical_active a WHERE a.physical_build_id=b.physical_build_id) THEN 0
 WHEN EXISTS(SELECT 1 FROM lexical_previous p WHERE p.physical_build_id=b.physical_build_id) THEN 1
 ELSE 2 END,b.physical_build_id LIMIT 1`

	var (
		result                                                                                        physicalLexicalManifest
		encoded                                                                                       []byte
		records, sources, conversations, actualRecords, actualFTS, actualSources, actualConversations uint64
	)

	err := snapshot.tx.QueryRowContext(snapshot.ctx, query, args...).Scan(
		&result.id, &result.manifest.GenerationID, &encoded, &records, &sources, &conversations,
		&actualRecords, &actualFTS, &actualSources, &actualConversations)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}

	if err != nil {
		return result, false, err
	}

	if records != actualRecords || records != actualFTS ||
		sources != actualSources || conversations != actualConversations {
		return result, false, ErrCorrupt
	}

	seq, err := decodeUint64(encoded)
	if err != nil {
		return result, false, err
	}

	result.manifest.Scope = scope
	result.manifest.ProjectionID = "sqlite"
	result.manifest.Watermark = seq
	result.manifest.Status = memory.ProjectionGenerationActive

	return result, true, nil
}

func (store *Store) canonicalManifest(ctx context.Context,
	scope memory.Scope) (memory.ProjectionManifest, bool, error) {
	manifest, found, err := store.canonicalReference(ctx, scope)
	if err != nil || !found || manifest.Status != memory.ProjectionGenerationActive {
		return memory.ProjectionManifest{}, false, err
	}

	return manifest, true, nil
}

// A dirty checkpoint cannot certify reads, but its exact complete physical
// build remains a durable reference during interrupted canonical activation.
func (store *Store) canonicalReference(ctx context.Context,
	scope memory.Scope) (memory.ProjectionManifest, bool, error) {
	store.mu.Lock()
	checkpoints := store.checkpoints
	store.mu.Unlock()

	if checkpoints == nil {
		return memory.ProjectionManifest{}, false, nil
	}

	manifest, found, err := checkpoints.LoadActiveProjectionManifest(ctx, scope, "sqlite")
	if err != nil || !found {
		return memory.ProjectionManifest{}, false, err
	}

	if manifest.Validate() != nil || manifest.Scope != scope || manifest.ProjectionID != "sqlite" {
		return memory.ProjectionManifest{}, false, nil
	}

	return manifest, true, nil
}

func sameProjectionTuple(left, right memory.ProjectionManifest) bool {
	return left.Scope == right.Scope && left.ProjectionID == right.ProjectionID &&
		left.GenerationID == right.GenerationID && left.Watermark == right.Watermark && left.Status == right.Status
}

// LoadActivePhysicalProjectionManifest requires both authoritative canonical
// state and a complete matching physical build, including empty generations.
//

func (store *Store) LoadActivePhysicalProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID string) (manifest memory.ProjectionManifest, found bool, result error) {
	defer func() { result = lexicalStorageError(result) }()

	if store == nil {
		return manifest, false, memory.PermanentError(memory.CodeDisabled, "sqlite projection is unavailable", nil)
	}

	if err := scope.Validate(); err != nil {
		return manifest, false, err
	}

	operation, done, err := store.acquire(ctx)
	if err != nil {
		return manifest, false, err
	}
	defer done()

	if projectionID != "sqlite" {
		return manifest, false, nil
	}

	manifest, found, err = store.canonicalManifest(operation, scope)
	if err != nil || !found {
		return manifest, false, err
	}

	snapshot, err := store.read(operation)
	if err != nil {
		return manifest, false, err
	}

	physical, found, err := snapshot.committedLexical(scope, manifest.GenerationID, manifest.Watermark, true)
	_ = snapshot.Close()

	if err != nil || !found {
		return memory.ProjectionManifest{}, false, err
	}

	valid, err := store.stillCertified(operation, physical, manifest)

	return manifest, valid, err
}

var _ memory.PhysicalProjectionViewReader = (*Store)(nil)
