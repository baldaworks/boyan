package semanticprojection

import (
	"context"
	"errors"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// InvalidateStale checks only an already-owned ready scope's certified metadata.
// It admits no work or new status entries; queued/in-flight repair remains owned
// by the scheduler. Rechecking after lexical sync also covers failed syncs and
// publications that completed during the attempt.
func (manager *Manager) InvalidateStale(ctx context.Context, scope memory.Scope) {
	if manager == nil || ctx == nil {
		return
	}

	key := scopeKey(scope)

	manager.statusMu.RLock()
	previous := manager.statuses[key]
	manager.statusMu.RUnlock()

	if previous.State != StateReady {
		return
	}

	view := semanticcore.View{
		Scope: scope, LexicalGenerationID: previous.LexicalGenerationID,
		LexicalWatermark: previous.LexicalWatermark, ModelSpace: previous.ModelSpace,
		Dimensions: manager.space.Dimensions,
	}
	err := manager.verifyView(ctx, view)
	// Caller cancellation leaves compatibility unknown, so it cannot justify
	// a persistent degradation of an otherwise certified ready view.
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	manager.statusMu.Lock()
	defer manager.statusMu.Unlock()
	// A newer build or schedule owns its transition. Never overwrite it with
	// the result of our earlier metadata observation.
	if manager.statuses[key] != previous {
		return
	}

	previous.State = StateDegraded
	previous.SafeReason = semanticcore.ReasonUnavailable
	previous.UpdatedAt = manager.now().UTC()
	manager.statuses[key] = previous
	manager.degradedCount++
}

// OperationalState returns a constant-work, text-free aggregate. Compatibility
// for a particular scope is still checked by Status and by the hybrid reader.
func (manager *Manager) OperationalState() State {
	manager.lifecycleMu.Lock()
	closed := manager.closed
	manager.lifecycleMu.Unlock()

	if closed {
		return StateStopped
	}

	manager.statusMu.RLock()
	defer manager.statusMu.RUnlock()

	if manager.repairingCount != 0 {
		return StateRepairing
	}

	if manager.degradedCount != 0 {
		return StateDegraded
	}

	return StateReady
}
