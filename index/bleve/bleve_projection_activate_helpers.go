package bleve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

func writeActiveGenerationMarker(markerPath, id string) error {
	temporaryPath := markerPath + ".tmp." + id
	if err := os.WriteFile(temporaryPath, []byte(id+"\n"), 0o600); err != nil {
		return fmt.Errorf("write bleve active marker: %w", err)
	}
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("activate bleve generation: %w", err)
	}
	return nil
}

func (p *BleveRecallProjection) swapActiveGeneration(scopeKey, id string, index bleve.Index, scoped bool) bleve.Index {
	p.mu.Lock()
	defer p.mu.Unlock()
	if scoped {
		previous := p.activeByScope[scopeKey]
		p.activeByScope[scopeKey] = index
		p.activeScopeIDs[scopeKey] = id
		return previous
	}
	previous := p.active
	p.active = index
	p.activeID = id
	return previous
}

func projectionScopeKey(scope memory.Scope) string {
	digest := sha256.Sum256([]byte(scope.Key + "\x00" + string(scope.Kind)))
	return hex.EncodeToString(digest[:])
}
