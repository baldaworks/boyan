package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const sourcePrivacyBatchSize = 32

type sourcePrivacyReadiness struct {
	Version         uint32 `json:"version"`
	CoverageVersion uint32 `json:"coverage_version"`
}

func requireSourcePrivacyReady(ctx context.Context, tx *Tx) error {
	var marker sourcePrivacyReadiness

	err := getPrivacyRecord(ctx, tx, Key{Family: SourcePrivacyReady}, SourcePrivacyReady, &marker)
	if privacyMissing(err) {
		return memory.RetryableError(
			memory.CodeUnavailable,
			"canonical source privacy is not prepared",
			nil,
		)
	}

	if err != nil || marker.Version != sourcePrivacyVersion ||
		marker.CoverageVersion != sourcePrivacyVersion {
		return invalidSourcePrivacy()
	}

	return nil
}

// SourcePrivacyReady checks the durable capability in an admitted snapshot.
func (o *Owner) SourcePrivacyReady(ctx context.Context) error {
	return o.View(ctx, requireSourcePrivacyReady)
}

// PrepareSourcePrivacy reconstructs bounded authority under exclusive admission.
// It commits each bounded page independently and publishes capability last.
// Failed preparation remains unavailable across reopening and restarts safely.
func (o *Owner) PrepareSourcePrivacy(ctx context.Context) error {
	if ctx == nil {
		return invalid("invalid canonical SQL context")
	}

	admitted, release, err := o.admit(ctx)
	if err != nil {
		return err
	}
	defer release()

	return o.prepareSourcePrivacy(admitted)
}

// privacyTransaction is used only while the owner already holds admission.
// It preserves session fencing between pages without recursive admission.
func (o *Owner) privacyTransaction(
	ctx context.Context,
	write bool,
	fn func(context.Context, *Tx) error,
) error {
	if err := o.checkPostgresOwnership(ctx); err != nil {
		return err
	}
	defer o.fenceBrokenPostgres()

	callback := func(tx *Tx) error { return fn(ctx, tx) }
	if write {
		return o.connection.Update(ctx, callback)
	}

	return o.connection.View(ctx, callback)
}

func (o *Owner) prepareSourcePrivacy(ctx context.Context) error {
	ready, err := o.preparedPrivacy(ctx)
	if err != nil || ready {
		return err
	}

	if err := o.privacyTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
			return err
		}

		return tx.Delete(ctx, Key{Family: SourcePrivacyReady})
	}); err != nil {
		return err
	}
	// Validate retained complete captures before reconstruction can overwrite
	// them. In-progress aggregates are partial and never grant readiness.
	for _, family := range []Family{SourcePrivacy, SourcePrivacyScope, TurnCertificate, Message} {
		if err := o.preparePrivacyFamily(ctx, family, validatePrivacyRow); err != nil {
			return err
		}
	}

	passes := []struct {
		family Family
		visit  func(context.Context, *Tx, Key, memory.Scope, string) error
	}{
		{Source, initializeHistoricalSourcePrivacy},
		{Message, collectHistoricalPrivacyMessage},
		{Revision, collectHistoricalPrivacyRevision},
		{Source, completeHistoricalSourcePrivacy},
	}
	for _, pass := range passes {
		if err := o.preparePrivacyFamily(ctx, pass.family, pass.visit); err != nil {
			return err
		}
	}

	return o.privacyTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
		if err := tx.checkIntegrity(ctx, sourcePrivacyBatchSize); err != nil {
			return err
		}

		return putPrivacyRecord(ctx,
			tx,
			Key{Family: SourcePrivacyReady},
			SourcePrivacyReady,
			sourcePrivacyReadiness{Version: sourcePrivacyVersion,
				CoverageVersion: sourcePrivacyVersion})
	})
}

func (o *Owner) preparePrivacyFamily(ctx context.Context,
	family Family,
	visit func(context.Context,
		*Tx,
		Key,
		memory.Scope,
		string) error) error {
	var after *Key

	for {
		var page []Row

		err := o.privacyTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			var err error

			page, err = tx.Scan(
				ctx,
				Range{Family: family, After: after, Limit: sourcePrivacyBatchSize},
			)
			if err != nil {
				return err
			}

			for _, row := range page {
				if err := visit(
					ctx,
					tx,
					row.Key,
					row.Key.Scope,
					string(row.Key.Parts[0]),
				); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil || len(page) == 0 {
			return err
		}

		after = &page[len(page)-1].Key
	}
}

func (o *Owner) preparedPrivacy(ctx context.Context) (bool, error) {
	err := o.privacyTransaction(ctx, false, requireSourcePrivacyReady)
	if err == nil {
		return true, nil
	}

	if code, _, ok := memory.ClassifyError(err); !ok || code != memory.CodeUnavailable {
		return false, err
	}

	return false, nil
}
