package canonicalsql

import (
	"context"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5"
)

// Trace only completed native queries. The writer commits after the head rows
// close, before ScanActiveMemory loads the selected item/current revision.
type activeSnapshotProbe struct {
	envelopes int
	heads     int
	afterHead func()
}

func (p *activeSnapshotProbe) finish(statement string, count int) {
	if !strings.Contains(statement, "envelope") {
		return
	}

	p.envelopes += count
	if strings.Contains(statement, "FROM head ") || strings.Contains(statement, "FROM boyan_canonical.head ") {
		p.heads += count
		if p.afterHead != nil {
			after := p.afterHead
			p.afterHead = nil

			after()
		}
	}
}

type activeSnapshotQuery struct {
	base  query
	probe *activeSnapshotProbe
}

func (q activeSnapshotQuery) exec(ctx context.Context, statement string, args ...any) (int64, error) {
	return q.base.exec(ctx, statement, args...)
}

func (q activeSnapshotQuery) query(ctx context.Context, statement string, args ...any) (rows, error) {
	r, err := q.base.query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}

	return &activeSnapshotRows{rows: r, probe: q.probe, statement: statement}, nil
}

type activeSnapshotRows struct {
	rows

	probe     *activeSnapshotProbe
	statement string
	count     int
	closed    bool
}

func (r *activeSnapshotRows) Next() bool {
	next := r.rows.Next()
	if next {
		if envelope, _ := observedEnvelopeRow(r.rows); envelope {
			r.count++
		}
	}

	return next
}

func (r *activeSnapshotRows) Close() error {
	err := r.rows.Close()
	if !r.closed {
		r.closed = true
		r.probe.finish(r.statement, r.count)
	}

	return err
}

func openActiveSnapshotOwner(t *testing.T, db *canonicalsqltest.Database) *Owner {
	t.Helper()

	if db.SQLite != nil {
		owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: db.DSN})
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})

		return owner
	}

	config := db.Postgres.Config()

	native, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatal("connect traced native reader")
	}

	c := NewPostgres(native)
	if err := acquirePostgresOwnership(t.Context(), c); err != nil {
		_ = native.Close(t.Context())
		t.Fatal(err)
	}

	owner := newOwner(c, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()

		return native.Close(ctx)
	})

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	return owner
}

func TestActiveMemorySnapshotAndRelatedHydrationBound(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		writer := testConnection(t, db)
		probe := &activeSnapshotProbe{}
		owner := openActiveSnapshotOwner(t, db)
		scope := testScope()
		seedActivePage(t, owner, scope, []string{"z", "aa", "zz"})

		probe.envelopes, probe.heads = 0, 0
		probe.afterHead = func() {
			updateActiveSnapshotItem(t, writer, scope)
		}

		var page []memory.ActiveCanonicalMemory

		err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			tx.q = activeSnapshotQuery{base: tx.q, probe: probe}

			var err error

			page, err = scanActiveMemoryTx(ctx, tx, memory.ActiveMemoryScanRequest{Scope: scope, Limit: 1})

			return err
		})
		assertActiveSnapshotPage(t, page, err, "key")

		if probe.afterHead != nil || probe.heads != 1 || probe.envelopes != 3 {
			t.Fatalf("snapshot interleave/bounds: head=%d related=%d writer_pending=%v",
				probe.heads, probe.envelopes-probe.heads, probe.afterHead != nil)
		}

		t.Logf("head_envelopes=%d related_envelopes=%d page_limit=1", probe.heads, probe.envelopes-probe.heads)

		page, err = owner.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
		assertActiveSnapshotPage(t, page, err, "committed-later")
	})
}

func updateActiveSnapshotItem(t *testing.T, writer *Connection, scope memory.Scope) {
	t.Helper()

	if err := writer.Update(t.Context(), func(tx *Tx) error {
		item, err := loadProofItem(t.Context(), tx, scope, "z")
		if err != nil {
			return err
		}

		item.MemoryKey = "committed-later"

		return tx.Put(t.Context(), Key{Family: Item, Scope: scope, Parts: [][]byte{[]byte("z")}},
			envelope(t, Item, item))
	}); err != nil {
		t.Fatal(err)
	}
}

func assertActiveSnapshotPage(t *testing.T, page []memory.ActiveCanonicalMemory, err error, key memory.MemoryKey) {
	t.Helper()

	if err != nil || len(page) != 1 || page[0].Item.ItemID != "z" || page[0].Item.MemoryKey != key ||
		len(page[0].Evidence) != 1 {
		t.Fatalf("active snapshot=%+v want key=%q err=%v", page, key, err)
	}
}
