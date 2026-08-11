package scopelanes

import memory "github.com/baldaworks/boyan/pkg/memory"

func (e *Executor) runLane(key string, lane *scopeLane) {
	defer e.laneWG.Done()
	for {
		e.mu.Lock()
		if len(lane.pending) == 0 {
			lane.running = false
			if e.lanes[key] == lane {
				delete(e.lanes, key)
			}
			e.signalLaneChangeLocked()
			e.mu.Unlock()
			return
		}
		work := lane.pending[0]
		lane.pending[0] = scopeLaneWork{}
		lane.pending = lane.pending[1:]
		e.signalLaneChangeLocked()
		e.mu.Unlock()

		select {
		case e.semaphore <- struct{}{}:
			err := work.run(work.ctx)
			<-e.semaphore
			work.done <- err
			close(work.done)
		case <-work.ctx.Done():
			work.done <- memory.RetryableError(memory.CodeTimeout, "scope lane context ended", work.ctx.Err())
			close(work.done)
		}
	}
}

func (e *Executor) signalLaneChangeLocked() {
	close(e.laneChanged)
	e.laneChanged = make(chan struct{})
}

func scopeLaneKey(scope memory.Scope) string {
	return string(scope.Kind) + "\x00" + scope.Key
}
