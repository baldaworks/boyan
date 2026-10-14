package bleve

import "github.com/blevesearch/bleve/v2"

// Close releases every active scope index and the unscoped process-wide index.
func (p *BleveRecallProjection) Close() error {
	if p == nil {
		return nil
	}

	active, activeID, scoped, scopedIDs := p.detachActiveIndices()

	var first error

	if active != nil {
		if err := active.Close(); err != nil {
			first = err
		}
	}

	for key, index := range scoped {
		if index == nil || (active != nil && scopedIDs[key] == activeID) {
			continue
		}

		if err := index.Close(); err != nil && first == nil {
			first = err
		}
	}

	return first
}

func (p *BleveRecallProjection) detachActiveIndices() (bleve.Index, string, map[string]bleve.Index, map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := p.active
	activeID := p.activeID
	scoped := make(map[string]bleve.Index, len(p.activeByScope))

	scopedIDs := make(map[string]string, len(p.activeScopeIDs))
	for key, index := range p.activeByScope {
		scoped[key] = index
		scopedIDs[key] = p.activeScopeIDs[key]
	}

	p.active = nil
	p.activeID = ""
	p.activeByScope = make(map[string]bleve.Index)
	p.activeScopeIDs = make(map[string]string)
	p.closed = true

	return active, activeID, scoped, scopedIDs
}
