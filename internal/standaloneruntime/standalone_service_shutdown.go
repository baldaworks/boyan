package standaloneruntime

import (
	"context"
)

func (s *standaloneService) stopComponents(ctx context.Context) error {
	var firstErr error
	recordStopError(&firstErr, s.shutdownServer(ctx))
	s.operations.Wait()
	recordStopError(&firstErr, s.sequenced.Close(ctx))

	if s.semantic != nil {
		recordStopError(&firstErr, s.semantic.close(ctx))
	}

	recordStopError(&firstErr, s.projection.Close(ctx))
	recordStopError(&firstErr, s.invoker.Close(ctx))
	recordStopError(&firstErr, s.index.Close())
	recordStopError(&firstErr, s.maintenance.Close(ctx))

	return firstErr
}
