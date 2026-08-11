package bleve

// Config controls the disposable Bleve projection adapter. The canonical
// store and application runtime own all semantic limits; this option only
// controls how many pending documents are flushed in one Bleve batch.
type Config struct {
	BatchSize int
}

// DefaultConfig returns the portable projection defaults.
func DefaultConfig() Config {
	return Config{BatchSize: bleveBatchSize}
}
