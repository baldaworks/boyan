//go:build integration

package canonicalsql

import (
	"context"
	"strings"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Count successfully scanned native values, including identity probes separately
// from envelopes. A query that returns nothing cannot satisfy these controls.
type eraseWorkQuery struct {
	base                              query
	pages, points, probes, downstream map[Family]int
	otherProbes                       int
}

func (q *eraseWorkQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q *eraseWorkQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	r, err := q.base.query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}

	var family Family

	for _, candidate := range durableFamilies {
		def, _ := familyDefinition(candidate)
		if strings.Contains(statement, "FROM "+def.table+" ") ||
			strings.Contains(statement, "FROM boyan_canonical."+def.table+" ") ||
			strings.Contains(statement, "FROM boyan_canonical."+def.table+"\n") {
			family = candidate

			break
		}
	}

	return &eraseWorkRows{rows: r, owner: q, family: family,
		envelope: strings.Contains(statement, "envelope"), page: strings.Contains(statement, "ORDER BY")}, nil
}

type eraseWorkRows struct {
	rows

	owner          *eraseWorkQuery
	family         Family
	envelope, page bool
}

func (r *eraseWorkRows) Scan(dest ...any) error {
	if err := r.rows.Scan(dest...); err != nil {
		return err
	}

	envelope, _ := observedEnvelopeRow(r.rows)
	switch {
	case !r.envelope && r.family != "" && r.page:
		r.owner.probes[r.family]++
	case !r.envelope:
		r.owner.otherProbes++
	case !envelope:
	case !canonicalEraseFamily(r.family):
		r.owner.downstream[r.family]++
	case r.page:
		r.owner.pages[r.family]++
	default:
		r.owner.points[r.family]++
	}

	return nil
}

func canonicalEraseFamily(family Family) bool {
	return family == Source || family == Message || family == Revision
}

func runObservedErase(t *testing.T, owner *Owner, request memory.ScopeEraseRequest) (memory.ScopeEraseJournal, error) {
	t.Helper()
	journal, err := owner.BeginScopeErase(t.Context(), request)
	work := &eraseWorkQuery{pages: make(map[Family]int), points: make(map[Family]int),
		probes: make(map[Family]int), downstream: make(map[Family]int)}

	for err == nil && !eraseTerminal(journal.Stage) {
		err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			work.base = tx.q
			tx.q = work

			current, found, err := loadScopeEraseTxn(ctx, tx, request.Scope, request.OperationID)
			if err != nil {
				return err
			}

			if !found {
				t.Fatal("erase journal disappeared")
			}

			if err := validateScopeEraseOwnership(ctx, tx, current); err != nil {
				return err
			}

			if err := advanceScopeEraseTxn(ctx, tx, &current); err != nil {
				return err
			}

			if err := putScopeEraseJournal(ctx, tx, current); err != nil {
				return err
			}

			journal = current

			return nil
		})
	}

	if err == nil {
		assertEraseWork(t, work, owner.connection.pg != nil)
	}

	return journal, err
}

func assertEraseWork(t *testing.T, work *eraseWorkQuery, postgres bool) {
	t.Helper()
	t.Logf("canonical pages=%v point_envelopes=%v page_identity_probes=%v other_probes=%d downstream_envelopes=%v",
		work.pages, work.points, work.probes, work.otherProbes, work.downstream)

	for family, want := range map[Family]int{Source: 4100, Message: 3075, Revision: 4100} {
		if work.pages[family] != want || work.points[family] != 0 {
			t.Errorf("family=%s pages=%d want%d point_envelopes=%d want0",
				family, work.pages[family], want, work.points[family])
		}

		if postgres && work.probes[family] != want {
			t.Errorf("family=%s identity_probes=%d want%d", family, work.probes[family], want)
		}
	}

	// This historical fixture has no active Head. The retained core projection
	// cases separately exercise head removal and live/reopened admission.
	for _, family := range []Family{Payload, DeniedSource, DeniedRevision, EraseBlobLock, EraseLockRef} {
		if work.downstream[family] == 0 {
			t.Errorf("missing required downstream family=%s", family)
		}
	}
}
