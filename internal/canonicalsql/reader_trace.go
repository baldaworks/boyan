package canonicalsql

import (
	"context"
	"errors"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type traceView struct {
	canonicalRecallView

	compat *memory.CanonicalCompatibilityPayload
}

type traceNode struct {
	view traceView
	text []byte
}

type traceGraph struct {
	nodes        []traceNode
	sourcesByRef map[memory.SourceRef]memory.SourceRecord
	rawByID      map[string]memory.SourceRecord
	captured     readerSnapshot
}

// Trace publishes a closed bounded graph after a fresh privacy barrier.
func (o *Owner) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if err := readerContext(ctx, o); err != nil {
		return memory.TraceResponse{}, err
	}

	request, err := memory.NormalizeTraceRequest(request, memory.MaxTraceNodes)
	if err != nil {
		return memory.TraceResponse{}, err
	}

	graph, err := o.preflightTraceGraph(ctx, request)
	if err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return hydrateTraceGraph(ctx, tx, request, &graph)
	})
	if err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	response, err := buildTraceResponse(request, graph)
	if err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	if err := o.finalTracePrivacyBarrier(ctx, graph); err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	return response, nil
}

func publicTraceError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if privacyMissing(err) {
		return readerDenied()
	}

	code, class, ok := memory.ClassifyError(err)
	if !ok {
		return invalidSourcePrivacy()
	}

	if class == memory.ErrorClassRetryable {
		return memory.RetryableError(code, "canonical trace is unavailable", nil)
	}

	return memory.PermanentError(code, "canonical trace is unavailable", nil)
}

// All reads needed for publication finish inside this fresh transaction.
func (o *Owner) finalTracePrivacyBarrier(ctx context.Context, graph traceGraph) error {
	return o.View(ctx, func(ctx context.Context, tx *Tx) error {
		if err := recheckTraceGraph(ctx, tx, graph); err != nil {
			return err
		}

		for id, previous := range graph.rawByID {
			current, err := loadTraceSourceRecord(ctx, tx, graph.captured.sources[id])
			if err != nil {
				return err
			}

			if !reflect.DeepEqual(previous, current) {
				return readerStateChanged()
			}
		}

		return ctx.Err()
	})
}

func recheckTraceGraph(ctx context.Context, tx *Tx, graph traceGraph) error {
	state, err := traceScopeState(ctx, tx, graph.captured.state.Scope)
	if err != nil {
		return err
	}

	if err := tracePayloadBounds(ctx, tx, graph.captured); err != nil {
		return err
	}

	if err := recheckReaderSources(ctx, tx, graph.captured); err != nil {
		return err
	}

	if err := recheckTraceViews(ctx, tx, graph); err != nil {
		return err
	}

	if state.Version != graph.captured.state.Version || state.ChangeSeq != graph.captured.state.ChangeSeq {
		return readerStateChanged()
	}

	return validateReaderSnapshotEvidence(ctx, tx, graph.captured)
}

func recheckTraceViews(ctx context.Context, tx *Tx, graph traceGraph) error {
	for _, node := range graph.nodes {
		current, err := loadTraceViewCore(ctx, tx, graph.captured.state.Scope, node.view.revision.RevisionID)
		if err != nil {
			return err
		}

		if !reflect.DeepEqual(current.revision, node.view.revision) ||
			!reflect.DeepEqual(current.item, node.view.item) || current.state != node.view.state {
			return readerStateChanged()
		}
	}

	return nil
}
