package badger

type canonicalLogicalImportBatcher struct {
	records   []CanonicalLogicalExportRecord
	onBatch   func([]CanonicalLogicalExportRecord) error
	batchSize int
}

func newCanonicalLogicalImportBatcher(batchSize int, onBatch func([]CanonicalLogicalExportRecord) error) canonicalLogicalImportBatcher {
	return canonicalLogicalImportBatcher{
		records:   make([]CanonicalLogicalExportRecord, 0, batchSize),
		onBatch:   onBatch,
		batchSize: batchSize,
	}
}

func (b *canonicalLogicalImportBatcher) add(record CanonicalLogicalExportRecord) error {
	b.records = append(b.records, record)
	if len(b.records) < b.batchSize {
		return nil
	}
	return b.flush()
}

func (b *canonicalLogicalImportBatcher) flush() error {
	if len(b.records) == 0 || b.onBatch == nil {
		b.records = b.records[:0]
		return nil
	}
	batch := append([]CanonicalLogicalExportRecord(nil), b.records...)
	if err := b.onBatch(batch); err != nil {
		return err
	}
	b.records = b.records[:0]
	return nil
}
