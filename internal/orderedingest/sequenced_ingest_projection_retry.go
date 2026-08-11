package orderedingest

import (
	"time"
)

func normalizeSequencedProjectionRetryDelay(config SequencedIngestConfig) time.Duration {
	retryDelay := config.ProjectionRetryDelay
	if retryDelay <= 0 {
		return defaultProjectionRepairRetryDelay
	}
	return retryDelay
}
