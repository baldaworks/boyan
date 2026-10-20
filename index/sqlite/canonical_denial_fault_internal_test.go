//go:build integration

//nolint:cyclop,gocognit,funlen,lll // Fault matrices retain explicit boundary and rollback expectations beside real SQL fixtures.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
	sqlitedriver "modernc.org/sqlite"
)

type denialFaultAuthority struct {
	canonicalPrivacyReader

	before func(context.Context)
	check  func(context.Context)
	load   func(context.Context, memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error)
}

func (a *denialFaultAuthority) IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	if a.check != nil {
		a.check(ctx)
	}

	if a.before != nil {
		a.before(ctx)
	}

	return a.canonicalPrivacyReader.IsRevisionDenied(ctx, scope, id)
}
func (a *denialFaultAuthority) LoadCanonicalRevisions(ctx context.Context, req memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	if a.check != nil {
		a.check(ctx)
	}

	if a.load != nil {
		return a.load(ctx, req)
	}

	return a.canonicalPrivacyReader.LoadCanonicalRevisions(ctx, req)
}

func (a *denialFaultAuthority) IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	if a.check != nil {
		a.check(ctx)
	}

	return a.canonicalPrivacyReader.IsSourceDenied(ctx, scope, id)
}

func TestCanonicalDenialReconcileFailureBoundaries(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		t.Run("caller cancellation beats authority failure", func(t *testing.T) {
			s := newCanonical(t)
			store, _ := openTestStore(t, false)
			denialCanonicalRevision(t, s, lexicalTestScope, "target", "source")
			seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle"}})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			a := &denialFaultAuthority{canonicalPrivacyReader: s, load: func(context.Context, memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
				cancel()

				return nil, memory.PermanentError(memory.CodeStoreFailure, "authority failure", nil)
			}}
			if err := store.bindCanonicalPrivacy(a); err != nil {
				t.Fatal(err)
			}

			if err := store.ReconcileCanonicalDenials(ctx, lexicalTestScope); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost %v", err)
			}

			assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)
		})
		t.Run("unknown owner FTS", func(t *testing.T) {
			s := newCanonical(t)

			store, _ := openTestStore(t, false)
			if err := store.bindCanonicalPrivacy(s); err != nil {
				t.Fatal(err)
			}

			execTest(t, store.writer, `INSERT INTO lexical_fts(rowid,raw_tokens) VALUES(999,'private orphan')`)

			if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("orphan accepted: %v", err)
			}

			if err := store.ReconcileAllCanonicalDenials(t.Context()); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("all-scope orphan accepted: %v", err)
			}
		})

		for _, fault := range []string{"missing structure", "wrong structure", "malformed evidence"} {
			t.Run(fault, func(t *testing.T) {
				s := newCanonical(t)
				store, _ := openTestStore(t, false)
				seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle"}})

				a := &denialFaultAuthority{canonicalPrivacyReader: s}
				if fault != "missing structure" {
					a.load = func(context.Context, memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
						r := memory.MemoryRevision{RevisionID: "wrong"}
						if fault == "malformed evidence" {
							r.RevisionID = "target"
							r.Evidence = []memory.EvidenceRef{{SourceID: ""}}
						}

						return []memory.MemoryRevision{r}, nil
					}
				}

				if err := store.bindCanonicalPrivacy(a); err != nil {
					t.Fatal(err)
				}

				if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err == nil {
					t.Fatal("invalid authority accepted")
				}

				assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)
				assertPrivacyCount(t, store, `SELECT revoked FROM lexical_build`, 0)
			})
		}

		for _, boundary := range []string{"BEFORE UPDATE ON lexical_build", "AFTER UPDATE ON lexical_build", "BEFORE DELETE ON lexical_record", "BEFORE DELETE ON lexical_source", "BEFORE DELETE ON lexical_conversation",
			"BEFORE UPDATE ON semantic_build", "AFTER UPDATE ON semantic_build", "BEFORE DELETE ON semantic_chunk", "BEFORE DELETE ON semantic_source", "BEFORE DELETE ON semantic_conversation", "BEFORE DELETE ON semantic_coverage"} {
			t.Run(boundary, func(t *testing.T) {
				s := newCanonical(t)
				store, _ := openTestStore(t, true)
				seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle", sources: []string{"source"}, conversations: []string{"conversation"}}})
				seedDenialSemantic(t, store, lexicalTestScope, "pending", "target", false, true)

				if err := store.bindCanonicalPrivacy(s); err != nil {
					t.Fatal(err)
				}

				if err := s.DenyRevision(t.Context(), lexicalTestScope, "target", lexicalTestTime); err != nil {
					t.Fatal(err)
				}

				execTest(t, store.writer, `CREATE TRIGGER denial_fault `+boundary+` BEGIN SELECT RAISE(ABORT,'private driver text'); END`)

				err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope)
				if !privacyCode(err, memory.CodeStoreFailure) || strings.Contains(err.Error(), "private") {
					t.Fatalf("fault lost: %v", err)
				}

				if strings.Contains(boundary, "semantic_") {
					assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_chunk`, 1)
					assertPrivacyCount(t, store, `SELECT complete FROM semantic_build`, 1)
				}

				execTest(t, store.writer, `DROP TRIGGER denial_fault`)

				if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
					t.Fatal(err)
				}

				assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 0)
				assertPrivacyCount(t, store, `SELECT count(*) FROM semantic_coverage`, 0)
			})
		}
	})
}

func TestCanonicalDenialReconcilePageCancellation(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		name := releaseSQLFunction(t, cancel)
		s := newCanonical(t)
		store, _ := openTestStore(t, false)

		var fixtures []lexicalFixture

		for i := range 257 {
			id := fmt.Sprintf("target-%03d", i)

			fixtures = append(fixtures, lexicalFixture{id: id, raw: "needle"})
			if err := s.DenyRevision(t.Context(), lexicalTestScope, id, lexicalTestTime); err != nil {
				t.Fatal(err)
			}
		}

		seedLexical(t, store, "owner", lexicalTestScope, fixtures)

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		execTest(t, store.writer, `CREATE TRIGGER denial_cancel BEFORE DELETE ON lexical_record WHEN OLD.rowid=129 BEGIN SELECT `+name+`(); END`)

		if err := store.ReconcileCanonicalDenials(ctx, lexicalTestScope); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel lost %v", err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 129)
		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_fts`, 129)
		assertPrivacyCount(t, store, `SELECT revoked FROM lexical_build`, 1)
		execTest(t, store.writer, `DROP TRIGGER denial_cancel`)

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 0)
	})
}

func TestCanonicalDenialReconcileCloseDrains(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, boundary := range []string{"scope", "read close", "canonical", "page SQL"} {
			t.Run(boundary, func(t *testing.T) {
				s := newCanonical(t)

				var store *Store

				started, release := make(chan struct{}), make(chan struct{})

				var function string
				if boundary == "page SQL" {
					function = releaseSQLFunction(t, func() { close(started); <-release })
				}

				if boundary == "read close" {
					store = openGatedReadStore(t, &gateRollbackDriver{started: started, release: release})
				} else {
					store, _ = openTestStore(t, false)
				}

				seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle"}})

				if err := s.DenyRevision(t.Context(), lexicalTestScope, "target", lexicalTestTime); err != nil {
					t.Fatal(err)
				}

				a := &denialFaultAuthority{canonicalPrivacyReader: s}
				if boundary == "canonical" {
					a.before = func(ctx context.Context) { close(started); <-ctx.Done() }
				}

				if err := store.bindCanonicalPrivacy(a); err != nil {
					t.Fatal(err)
				}

				var unlock func()

				if boundary == "scope" {
					var err error

					unlock, err = store.lockScope(t.Context(), lexicalTestScope)
					if err != nil {
						t.Fatal(err)
					}
				}

				if boundary == "page SQL" {
					execTest(t, store.writer, `CREATE TRIGGER denial_pause BEFORE DELETE ON lexical_record BEGIN SELECT `+function+`(); END`)
				}

				result := make(chan error, 1)
				go func() { result <- store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope) }()

				if boundary == "scope" {
					deadline := time.Now().Add(3 * time.Second)
					stack := make([]byte, 1<<20)

					for {
						n := runtime.Stack(stack, true)
						if strings.Contains(string(stack[:n]), "(*Store).lockScope") {
							break
						}

						if time.Now().After(deadline) {
							t.Fatal("scope admission not started")
						}

						runtime.Gosched()
					}
				} else {
					select {
					case <-started:
					case <-time.After(3 * time.Second):
						t.Fatal("boundary not reached")
					}
				}

				closeCtx := t.Context()

				cancel := func() {}
				if boundary == "read close" || boundary == "page SQL" {
					closeCtx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
				}
				defer cancel()

				err := store.CloseContext(closeCtx)
				if boundary == "read close" || boundary == "page SQL" {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("close failed to retain resources %v", err)
					}

					select {
					case <-store.closed:
						t.Fatal("pools closed before blocked owner drained")
					default:
					}

					close(release)
				} else if err != nil {
					t.Fatal(err)
				}

				if unlock != nil {
					unlock()
				}

				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
						t.Fatalf("close result %v", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("operation did not drain")
				}

				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

// This driver delegates actual reads to SQLite and changes only the selected
// rowset failure. It never supplies permissive authority or synthetic data.
type denialReadFault struct {
	target            string
	closeErr, nextErr error
}

func (f *denialReadFault) Open(name string) (driver.Conn, error) {
	c, err := (&sqlitedriver.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}

	return &denialFaultConn{Conn: c, f: f}, nil
}

type denialFaultConn struct {
	driver.Conn

	f *denialReadFault
}

func (c *denialFaultConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *denialFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		return nil, err
	}

	if strings.Contains(q, c.f.target) {
		return &denialFaultRows{Rows: r, f: c.f}, nil
	}

	return r, nil
}

type denialFaultRows struct {
	driver.Rows

	f *denialReadFault
}

func (r *denialFaultRows) Close() error {
	err := r.Rows.Close()
	if err != nil {
		return err
	}

	return r.f.closeErr
}
func (r *denialFaultRows) Next(values []driver.Value) error {
	if r.f.nextErr != nil {
		return r.f.nextErr
	}

	return r.Rows.Next(values)
}

func TestCanonicalDenialReconcileSnapshotCloseError(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		release := make(chan struct{})
		close(release)
		store := openGatedReadStore(t, &gateRollbackDriver{started: make(chan struct{}), release: release, rollbackErr: memory.RetryableError(memory.CodeConflict, "snapshot close failure", nil)})
		seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle"}})

		if err := s.DenyRevision(t.Context(), lexicalTestScope, "target", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		calls := 0

		a := &denialFaultAuthority{canonicalPrivacyReader: s, before: func(context.Context) { calls++ }}
		if err := store.bindCanonicalPrivacy(a); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); !privacyRetryableConflict(err) {
			t.Fatalf("snapshot close hidden: %v", err)
		}

		if calls != 0 {
			t.Fatalf("canonical calls after snapshot Close error %d", calls)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)
	})
}

func TestCanonicalDenialReconcileBusyIsRetryable(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		s := newCanonical(t)
		store, dir := openTestStore(t, false)
		seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle"}})

		if err := store.bindCanonicalPrivacy(s); err != nil {
			t.Fatal(err)
		}

		if err := s.DenyRevision(t.Context(), lexicalTestScope, "target", lexicalTestTime); err != nil {
			t.Fatal(err)
		}

		other, err := sql.Open("sqlite", poolURI(filepath.Join(dir, "search", "search.sqlite"), false, false))
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = other.Close() }()

		tx, err := other.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(t.Context(), `UPDATE lexical_build SET record_count=1`); err != nil {
			t.Fatal(err)
		}

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); !privacyRetryableConflict(err) {
			t.Fatalf("busy classification %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}

		assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)

		if err := store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCanonicalDenialReconcileReadCloseErrors(t *testing.T) {
	runDenialAuthorities(t, func(t *testing.T, newCanonical denialOwnerFactory) {
		for _, membership := range []bool{false, true} {
			for _, primary := range []bool{false, true} {
				t.Run(fmt.Sprint(membership, primary), func(t *testing.T) {
					s := newCanonical(t)
					store, dir := openTestStore(t, false)
					denialCanonicalRevision(t, s, lexicalTestScope, "target", "safe")
					seedLexical(t, store, "owner", lexicalTestScope, []lexicalFixture{{id: "target", raw: "needle", sources: []string{"denied"}}})

					if err := s.DenySource(t.Context(), lexicalTestScope, "denied", lexicalTestTime); err != nil {
						t.Fatal(err)
					}

					calls := 0

					a := &denialFaultAuthority{canonicalPrivacyReader: s, before: func(context.Context) { calls++ }}
					if err := store.bindCanonicalPrivacy(a); err != nil {
						t.Fatal(err)
					}

					f := &denialReadFault{target: "SELECT r.rowid", closeErr: memory.RetryableError(memory.CodeConflict, "close failure", nil)}
					if membership {
						f.target = "SELECT id FROM lexical_source"
					}

					if primary {
						f.nextErr = memory.PermanentError(memory.CodeNotFound, "primary read failure", nil)
					}

					name := fmt.Sprintf("denial_read_%d", semanticCancelSequence.Add(1))
					sql.Register(name, f)

					reader, err := sql.Open(name, poolURI(filepath.Join(dir, "search", "search.sqlite"), true, false))
					if err != nil {
						t.Fatal(err)
					}

					reader.SetMaxOpenConns(readerConnections)

					if err := store.reader.Close(); err != nil {
						t.Fatal(err)
					}

					store.reader = reader

					err = store.ReconcileCanonicalDenials(t.Context(), lexicalTestScope)
					if primary {
						if !privacyCode(err, memory.CodeNotFound) {
							t.Fatalf("primary lost %v", err)
						}
					} else if !privacyRetryableConflict(err) {
						t.Fatalf("close lost %v", err)
					}

					if !membership && calls != 0 {
						t.Fatalf("callback after failed page close %d", calls)
					}

					assertPrivacyCount(t, store, `SELECT count(*) FROM lexical_record`, 1)
					assertPrivacyCount(t, store, `SELECT revoked FROM lexical_build`, 0)
				})
			}
		}
	})
}
