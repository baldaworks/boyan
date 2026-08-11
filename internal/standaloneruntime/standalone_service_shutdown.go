package standaloneruntime

import (
	"context"
)

func (s *standaloneService) stopComponents(ctx context.Context) error {
	var firstErr error
	recordStopError(&firstErr, s.shutdownServer(ctx))
	recordStopError(&firstErr, s.sequenced.Close(ctx))
	recordStopError(&firstErr, s.projection.Close(ctx))
	recordStopError(&firstErr, s.invoker.Close(ctx))
	recordStopError(&firstErr, s.applier.Close())
	recordStopError(&firstErr, s.maintenance.Close(ctx))
	return firstErr
}
