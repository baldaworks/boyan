package scopelanes

import memory "github.com/baldaworks/boyan/pkg/memory"

func (e *Executor) tryQueueScopeLaneWork(
	key string,
	work scopeLaneWork,
) (<-chan error, <-chan struct{}, bool, error) {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()

		return nil, nil, false, memory.PermanentError(memory.CodeDisabled, "scope lane executor is not started", nil)
	}

	if e.closed {
		e.mu.Unlock()

		return nil, nil, false, memory.PermanentError(memory.CodeShuttingDown, "scope lane executor is closing", nil)
	}

	lane := e.lanes[key]
	if lane == nil {
		lane = &scopeLane{}
		e.lanes[key] = lane
	}

	if len(lane.pending) >= e.config.MaxQueuedPerScope {
		changed := e.laneChanged
		e.mu.Unlock()

		return nil, changed, false, nil
	}

	lane.pending = append(lane.pending, work)
	if !lane.running {
		lane.running = true

		e.laneWG.Add(1)
		go e.runLane(key, lane)
	}

	e.signalLaneChangeLocked()

	done := (<-chan error)(work.done)
	e.mu.Unlock()

	return done, nil, true, nil
}
