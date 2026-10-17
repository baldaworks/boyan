package semanticprojection

import (
	"context"
	"errors"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

type hybridAvailability struct {
	manifest memory.ProjectionManifest
	state    semantic.State
	found    bool
}

// probeAvailability returns copied control metadata only. Closing here releases
// the transaction, reader and Store lease before authority checks or inference.
func (hybrid *Hybrid) probeAvailability(ctx context.Context,
	scope memory.Scope) (availability hybridAvailability, err error) {
	snapshot, err := hybrid.reader.OpenSearchSnapshot(ctx, scope)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		}

		return availability, err
	}
	defer func() {
		if closeErr := snapshot.Close(); err == nil {
			err = closeErr
		}

		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		}
	}()

	availability.manifest = snapshot.LexicalManifest()
	availability.state, availability.found, err = snapshot.SemanticState()
	// Operation/Close errors are fatal. Only this semantic control boundary may
	// observe ordinary unavailability. Actual classified storage failures and
	// wrapped cancellation stay errors even with a semantic identity as cause.
	_, _, classified := memory.ClassifyError(err)
	if err != nil && !classified && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) &&
		semanticUnavailable(err) {
		availability.found, err = false, nil
	}

	return availability, err
}
