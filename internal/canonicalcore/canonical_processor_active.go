package canonicalcore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) loadCanonicalActiveView(ctx context.Context,
	turn Turn,
	candidates []SemanticCandidate) (ScopeState,
	[]ActiveCanonicalMemory,
	[]ActiveMemoryItem,
	error) {
	state, err := p.store.LoadScopeState(ctx, turn.Scope)
	if err != nil {
		return ScopeState{}, nil, nil, err
	}

	if state.Scope != turn.Scope {
		return ScopeState{},
			nil,
			nil,
			PermanentError(CodeScopeViolation,
				"canonical scope state does not match turn",
				nil)
	}

	matchers, err := p.canonicalActiveMatchers(turn, candidates)
	if err != nil {
		return ScopeState{}, nil, nil, err
	}

	active, err := p.scanCanonicalMatches(ctx, turn.Scope, matchers)
	if err != nil {
		return ScopeState{}, nil, nil, err
	}

	view := make([]ActiveMemoryItem, len(active))
	for i, item := range active {
		view[i] = ActiveMemoryItem{Item: item.Item,
			Evidence: append([]EvidenceRef(nil),
				item.Evidence...)}
	}

	return state, active, view, nil
}

func (p *CanonicalTurnProcessor) canonicalActiveMatchers(turn Turn,
	candidates []SemanticCandidate) ([]ActiveMemoryMatcher,
	error) {
	messages := make(map[string]Message, len(turn.Messages))
	for _, message := range turn.Messages {
		messages[message.MessageID] = message
	}

	matchers := make([]ActiveMemoryMatcher, 0, len(candidates))
	for _, candidate := range candidates {
		canonical, err := canonicalizeCandidate(turn.SourceID, messages, candidate)
		if err != nil {
			return nil, err
		}

		matcher, err := p.policy.PrepareActiveMemoryMatcher(turn.Scope, canonical)
		if err != nil {
			return nil, err
		}

		matchers = append(matchers, matcher)
	}

	return matchers, nil
}

type canonicalMatches struct {
	matchers []ActiveMemoryMatcher
	matched  []bool
	active   []ActiveCanonicalMemory
	seen     map[string]struct{}
}

func (p *CanonicalTurnProcessor) scanCanonicalMatches(ctx context.Context,
	scope Scope,
	matchers []ActiveMemoryMatcher) ([]ActiveCanonicalMemory,
	error) {
	matches := canonicalMatches{matchers: matchers,
		matched: make([]bool,
			len(matchers)),
		active: make([]ActiveCanonicalMemory,
			0,
			len(matchers)),
		seen: make(map[string]struct{},
			len(matchers))}
	if len(matchers) == 0 {
		return matches.active, checkContext(ctx)
	}

	cursor := ""

	for {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}

		page,
			err := p.store.ScanActiveMemory(ctx,
			ActiveMemoryScanRequest{Scope: scope,
				AfterItemID: cursor,
				Limit:       maxCanonicalReadRecords})
		if err != nil {
			return nil, err
		}

		if len(page) == 0 {
			return matches.active, checkContext(ctx)
		}

		if len(page) > maxCanonicalReadRecords {
			return nil, limitExceeded("canonical active scan page exceeds limit")
		}

		cursor, err = matches.considerPage(ctx, scope, cursor, page)
		if err != nil {
			return nil, err
		}
	}
}

func (m *canonicalMatches) consider(active ActiveCanonicalMemory) error {
	retain := false

	for i, matcher := range m.matchers {
		matched, err := matcher.Matches(ActiveMemoryItem{Item: active.Item, Evidence: active.Evidence})
		if err != nil {
			return err
		}

		if !matched {
			continue
		}

		if m.matched[i] {
			return invalidDerived("reconciliation has ambiguous active matches")
		}

		m.matched[i] = true
		retain = true
	}

	if !retain {
		return nil
	}

	if _, seen := m.seen[active.Item.ItemID]; !seen {
		active.Evidence = append([]EvidenceRef(nil), active.Evidence...)
		m.active = append(m.active, active)
		m.seen[active.Item.ItemID] = struct{}{}
	}

	return nil
}

func (m *canonicalMatches) considerPage(ctx context.Context,
	scope Scope,
	cursor string,
	page []ActiveCanonicalMemory) (string,
	error) {
	for _, active := range page {
		if err := checkContext(ctx); err != nil {
			return "", err
		}

		if err := validateActiveCanonicalMemory(scope, active); err != nil {
			return "", err
		}

		id := active.Item.ItemID
		if len(id) < len(cursor) || len(id) == len(cursor) && id <= cursor {
			return "",
				PermanentError(CodeStoreFailure,
					"canonical active cursor did not advance",
					nil)
		}

		if err := m.consider(active); err != nil {
			return "", err
		}

		cursor = id
	}

	return cursor, nil
}
