package canonicalsql

import (
	"bytes"
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (tx *Tx) pageMetadataStatement(def familyDef) string {
	return `WITH requested AS MATERIALIZED (
 SELECT binary_key,ordinal FROM unnest($1::bytea[]) WITH ORDINALITY AS k(binary_key,ordinal)
)
SELECT k.ordinal,
 (SELECT count(*) FROM (SELECT 1 FROM ` + tx.connection.table(def.table) + `
                       WHERE logical_key=k.binary_key LIMIT 2) exact),
 s.ids,s.sequences,s.deleted
FROM requested k LEFT JOIN LATERAL (
 SELECT array_agg(stamp_id) AS ids,array_agg(sequence) AS sequences,array_agg(deleted) AS deleted
 FROM (SELECT stamp_id,CASE WHEN length(last_seq)=8 THEN last_seq ELSE NULL END AS sequence,deleted
       FROM ` + tx.connection.table("row_stamp") + ` WHERE family=$2 AND binary_key=k.binary_key LIMIT 2) bounded
) s ON true ORDER BY k.ordinal`
}

func (tx *Tx) checkActiveHeadIdentities(ctx context.Context, page []Row) error {
	def, _ := familyDefinition(Head)

	return tx.checkPageIdentities(ctx, Head, def, page)
}

func (tx *Tx) checkPageIdentities(ctx context.Context, f Family, def familyDef, page []Row) error {
	if len(page) == 0 {
		return nil
	}

	keys := make([][]byte, len(page))
	for i, row := range page {
		_, values, _ := row.Key.values()
		keys[i] = stampKey(values)
	}

	r, err := tx.q.query(ctx, tx.pageMetadataStatement(def), keys, []byte(f))
	if err != nil {
		return tx.fail(databaseError(ctx, "check page identities", err))
	}
	defer r.Close()

	count := 0

	for r.Next() {
		count++
		if err := checkPageMetadata(r, count); err != nil {
			return tx.fail(rowError(ctx, err))
		}
	}

	if err := r.Err(); err != nil {
		return tx.fail(databaseError(ctx, "check page identities", err))
	}

	if err := r.Close(); err != nil {
		return tx.fail(databaseError(ctx, "close page identities", err))
	}

	if count != len(page) {
		return tx.fail(corruptRow())
	}

	return nil
}

func checkPageMetadata(r rows, expected int) error {
	var (
		ordinal, matches int
		ids              []int64
		sequences        [][]byte
		deleted          []bool
	)

	if err := r.Scan(&ordinal, &matches, &ids, &sequences, &deleted); err != nil {
		return err
	}

	if ordinal != expected || matches != 1 || len(ids) > 1 || len(ids) != len(sequences) || len(ids) != len(deleted) {
		return corruptRow()
	}

	if len(ids) == 1 {
		_, err := decodePointStamp(&ids[0], sequences[0], &deleted[0])

		return err
	}

	return nil
}

// Both branches have Revision's physical column layout; Item's three absent
// materialized columns are NULL. Each LEFT JOIN supplies an explicit absence
// marker, and each original-byte row/stamp probe remains independently LIMIT2.
func (tx *Tx) activeRelatedBranch(f Family) string {
	def, _ := familyDefinition(f)
	keyColumn, familyTag, parameter := "item_key", "0", "$3"
	aliases := "row_id,scope_key,scope_kind,id,logical_key,scope_partition,envelope,size"
	projection := "r.row_id,r.scope_key,r.scope_kind,r.id,r.logical_key,r.scope_partition," +
		"NULL::bytea,r.envelope,r.size,NULL::bytea,NULL::bytea"

	if f == Revision {
		keyColumn, familyTag, parameter = "revision_key", "1", "$4"
		aliases = "row_id,scope_key,scope_kind,id,logical_key,scope_partition," +
			"item_partition,envelope,size,item_id,revision_u64"
		projection = "r.row_id,r.scope_key,r.scope_kind,r.id,r.logical_key,r.scope_partition," +
			"r.item_partition,r.envelope,r.size,r.item_id,r.revision_u64"
	}

	// row_id + raw/derived columns + envelope/size + Revision's two extras.
	const physicalColumns = 11

	return "SELECT k.ordinal," + familyTag + " AS family,0 AS probe," + projection +
		",NULL::bigint AS stamp_id,NULL::bytea AS last_seq,NULL::boolean AS deleted FROM requested k " +
		"LEFT JOIN LATERAL (SELECT " + pgSelection(f, def) + " FROM " + tx.connection.table(def.table) +
		" WHERE logical_key=k." + keyColumn + " LIMIT 2) AS r(" + aliases + ") ON true UNION ALL " +
		"SELECT k.ordinal," + familyTag + ",1," + strings.Repeat("NULL,", physicalColumns) +
		"s.stamp_id,s.last_seq,s.deleted FROM requested k LEFT JOIN LATERAL (SELECT stamp_id," +
		"CASE WHEN length(last_seq)=8 THEN last_seq ELSE NULL END AS last_seq,deleted FROM " +
		tx.connection.table("row_stamp") + " WHERE family=" + parameter + " AND binary_key=k." +
		keyColumn + " LIMIT 2) s ON true"
}

func (tx *Tx) activeRelatedStatement() string {
	return "WITH requested AS MATERIALIZED (SELECT item_key,revision_key,ordinal " +
		"FROM unnest($1::bytea[],$2::bytea[]) WITH ORDINALITY AS k(item_key,revision_key,ordinal)) " +
		tx.activeRelatedBranch(Item) + " UNION ALL " + tx.activeRelatedBranch(Revision) +
		" ORDER BY ordinal,family,probe"
}

type activeRelatedRecord struct {
	ordinal, family, probe int
	physical               *physicalRowScan
	stampID                *int64
	sequence               []byte
	deleted                *bool
}

type activeRelatedStream struct {
	rows

	next *activeRelatedRecord
}

func (s *activeRelatedStream) advance() error {
	s.next = nil
	if !s.Next() {
		return s.Err()
	}

	def, _ := familyDefinition(Revision)
	r := &activeRelatedRecord{physical: newPhysicalRowScan(Revision, def)}
	dest := append([]any{&r.ordinal, &r.family, &r.probe}, r.physical.destinations()...)
	dest = append(dest, &r.stampID, &r.sequence, &r.deleted)

	if err := s.Scan(dest...); err != nil {
		return err
	}

	s.next = r

	return nil
}

func (r *activeRelatedRecord) decodeRow(key Key) (*physicalRow, error) {
	if r.physical.id == nil {
		if r.physical.size != nil {
			return nil, corruptRow()
		}

		return nil, nil
	}

	def, _, _ := key.values()
	if key.Family == Item {
		r.physical.derived = r.physical.derived[:len(derivedColumns(Item, def))]
		r.physical.extra = nil
	}

	row, err := r.physical.decode(key.Family, def)
	if err != nil {
		return nil, err
	}

	_, stored, _ := row.Key.values()

	_, requested, _ := key.values()
	if !bytes.Equal(stampKey(stored), stampKey(requested)) {
		return nil, corruptRow()
	}

	return &row, nil
}

// Consume one ordered point completely before decoding its DTO. In particular,
// a missing physical row must still check its stamp before returning NotFound.
func (s *activeRelatedStream) point(ordinal int, key Key) ([]byte, error) {
	familyTag := 0
	if key.Family == Revision {
		familyTag = 1
	}

	var point activePointState

	for s.next != nil && s.next.ordinal == ordinal && s.next.family == familyTag {
		if err := point.accept(s.next, key); err != nil {
			return nil, err
		}

		if err := s.advance(); err != nil {
			return nil, err
		}
	}

	if !point.seenRow || !point.seenStamp {
		return nil, corruptRow()
	}

	if point.row == nil {
		return nil, memory.PermanentError(memory.CodeNotFound, "canonical SQL row not found", nil)
	}

	return point.row.Envelope, nil
}

// State is confined to one ordered native point, never cached across reads.
type activePointState struct {
	row                *physicalRow
	seenRow, seenStamp bool
}

func (p *activePointState) accept(r *activeRelatedRecord, key Key) error {
	switch r.probe {
	case 0:
		if p.seenRow {
			return corruptRow()
		}

		p.seenRow = true
		row, err := r.decodeRow(key)
		p.row = row

		return err
	case 1:
		if !p.seenRow || p.seenStamp {
			return corruptRow()
		}

		p.seenStamp = true

		return r.checkStamp()
	default:
		return corruptRow()
	}
}

func (r *activeRelatedRecord) checkStamp() error {
	if r.stampID == nil {
		if r.sequence != nil || r.deleted != nil {
			return corruptRow()
		}

		return nil
	}

	_, err := decodePointStamp(r.stampID, r.sequence, r.deleted)

	return err
}

// Defer invalid related-key errors to the original per-head processing order.
// A NULL requested key has no matches; it does not broaden any SQL predicate.
func activeRelatedKey(scope memory.Scope, f Family, id string) []byte {
	key, err := privacyKey(scope, f, id)
	if err != nil {
		return nil
	}

	_, values, _ := key.values()

	return stampKey(values)
}

func (tx *Tx) postgresActiveMemory(ctx context.Context, scope memory.Scope,
	heads []memory.ItemHead) (active []memory.ActiveCanonicalMemory, result error) {
	active = make([]memory.ActiveCanonicalMemory, 0, len(heads))
	if len(heads) == 0 {
		return active, nil
	}

	defer func() {
		if result != nil {
			active = nil
			result = tx.fail(rowError(ctx, result))
		}
	}()

	items, revisions := make([][]byte, len(heads)), make([][]byte, len(heads))
	for i, head := range heads {
		items[i] = activeRelatedKey(scope, Item, head.ItemID)
		revisions[i] = activeRelatedKey(scope, Revision, head.RevisionID)
	}

	r, err := tx.q.query(ctx, tx.activeRelatedStatement(), items, revisions, []byte(Item), []byte(Revision))
	if err != nil {
		return nil, databaseError(ctx, "read active related records", err)
	}
	defer r.Close()

	stream := &activeRelatedStream{rows: r}
	if err := stream.advance(); err != nil {
		return nil, err
	}

	for i, head := range heads {
		item, revision, err := stream.memory(scope, i+1, head)
		if err != nil {
			return nil, err
		}

		active = append(active, memory.ActiveCanonicalMemory{Item: item, RevisionID: revision.RevisionID,
			Revision: revision.Revision, Evidence: append([]memory.EvidenceRef(nil), revision.Evidence...)})
	}

	if stream.next != nil {
		return nil, corruptRow()
	}

	if err := r.Close(); err != nil {
		return nil, databaseError(ctx, "close active related records", err)
	}

	return active, nil
}

func (s *activeRelatedStream) memory(scope memory.Scope, ordinal int,
	head memory.ItemHead) (memory.MemoryItem, memory.MemoryRevision, error) {
	key, err := privacyKey(scope, Item, head.ItemID)
	if err != nil {
		return memory.MemoryItem{}, memory.MemoryRevision{}, err
	}

	data, err := s.point(ordinal, key)
	if err != nil {
		return memory.MemoryItem{}, memory.MemoryRevision{}, err
	}

	item, err := decodeProofItem(key, data)
	if err != nil {
		return item, memory.MemoryRevision{}, err
	}

	key, err = privacyKey(scope, Revision, head.RevisionID)
	if err != nil {
		return item, memory.MemoryRevision{}, err
	}

	data, err = s.point(ordinal, key)
	if err != nil {
		return item, memory.MemoryRevision{}, err
	}

	revision, err := decodeProofRevision(key, data)
	if err == nil && (revision.ItemID != item.ItemID || revision.RevisionID != head.RevisionID) {
		err = corruptRow()
	}

	return item, revision, err
}
