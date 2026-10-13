package bleve

import memory "github.com/baldaworks/boyan/pkg/memory"

// Config controls the disposable Bleve projection adapter. The canonical
// store and application runtime own semantic limits. BatchSize bounds writes;
// Checkpoints and ProjectionID optionally enable certified minimum-watermark reads.
type Config struct {
	BatchSize    int
	Checkpoints  memory.RecallProjectionCheckpointReader
	ProjectionID string
}

// DefaultConfig returns the portable projection defaults.
func DefaultConfig() Config {
	return Config{BatchSize: bleveBatchSize}
}
