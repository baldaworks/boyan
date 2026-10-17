package semanticprojection //nolint:testpackage // Promotes the TASK013 final-channel Close probe.

import (
	"errors"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

// Ignoring final-channel Close errors would release otherwise valid hybrid
// identities even when transaction completion failed. Preflight Close coverage
// cannot substitute for this separate, explicitly asserted channel phase.
func TestSQLiteHybridFinalChannelClosePreservesFailure(t *testing.T) {
	f, h, request := newSQLHybrid(t)
	failure := errors.New("private rollback details")

	wrapHybridChannelRead(t, h, func(s *hybridBoundarySnapshot) {
		s.SearchSnapshot = preflightFaultSnapshot{SearchSnapshot: s.SearchSnapshot,
			closeView: func() error { return failure }}
	})
	hits, report, err := h.SearchRecallWithReport(t.Context(), request)

	code, class, classified := memory.ClassifyError(err)
	if hits != nil || report != nil || !errors.Is(err, failure) || !classified ||
		code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent || strings.Contains(err.Error(), "private") {
		t.Fatalf("final Close lost/leaked failure: %#v %#v %v", hits, report, err)
	}

	assertHybridReadersReacquirable(t, f)
}
