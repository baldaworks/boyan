package bleve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

func openWithConfig(root string, config Config) (*BleveRecallProjection, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "bleve projection root is required", nil)
	}
	if config.BatchSize <= 0 {
		config.BatchSize = bleveBatchSize
	}
	if config.BatchSize > memory.MaxSnapshotItems {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "bleve batch size exceeds the projection bound", nil)
	}
	if err := os.MkdirAll(filepath.Join(root, bleveGenerationDirectory), 0o700); err != nil {
		return nil, fmt.Errorf("create bleve projection root: %w", err)
	}
	projection := &BleveRecallProjection{
		root:           root,
		batchSize:      config.BatchSize,
		activeByScope:  make(map[string]bleve.Index),
		activeScopeIDs: make(map[string]string),
	}
	marker, err := os.ReadFile(filepath.Join(root, bleveActiveGenerationFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read bleve active generation: %w", err)
	}
	if err == nil {
		id := strings.TrimSpace(string(marker))
		index, openErr := openCommittedBleveGeneration(root, id)
		if openErr != nil {
			return nil, openErr
		}
		projection.activeID = id
		projection.active = index
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		_ = projection.Close()
		return nil, fmt.Errorf("read bleve projection markers: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, bleveScopedActivePrefix) {
			continue
		}
		scopeKey := strings.TrimPrefix(name, bleveScopedActivePrefix)
		if strings.Contains(scopeKey, ".tmp.") {
			continue
		}
		if len(scopeKey) != sha256.Size*2 || !isLowerHex(scopeKey) {
			_ = projection.Close()
			return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve scoped active marker is invalid", nil)
		}
		marker, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			_ = projection.Close()
			return nil, fmt.Errorf("read bleve scoped active generation: %w", readErr)
		}
		id := strings.TrimSpace(string(marker))
		index, openErr := openCommittedBleveGeneration(root, id)
		if openErr != nil {
			_ = projection.Close()
			return nil, openErr
		}
		projection.activeByScope[scopeKey] = index
		projection.activeScopeIDs[scopeKey] = id
	}
	return projection, nil
}

func openCommittedBleveGeneration(root, id string) (bleve.Index, error) {
	if !validBleveGenerationID(id) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve active generation marker is invalid", nil)
	}
	path := filepath.Join(root, bleveGenerationDirectory, id)
	committed, err := os.ReadFile(filepath.Join(path, bleveCommittedGenerationFile))
	if err != nil || strings.TrimSpace(string(committed)) != BleveRecallProjectionSchema {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve active generation is not committed", err)
	}
	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bleve active generation: %w", err)
	}
	return index, nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// ActivateGeneration opens a committed generation and atomically switches the
// legacy process-wide active marker. New application wiring should use
// ActivateGenerationForScope so activating one exact scope cannot discard
// another scope's disposable generation.
func (p *BleveRecallProjection) ActivateGeneration(ctx context.Context, id string) error {
	return p.activateGeneration(ctx, memory.Scope{}, id, false)
}

// ActivateGenerationForScope opens a committed generation and switches the
// active marker for one exact scope. Generations remain disposable indexes;
// canonical state is still the source of truth during recall hydration.
func (p *BleveRecallProjection) ActivateGenerationForScope(ctx context.Context, scope memory.Scope, id string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	return p.activateGeneration(ctx, scope, id, true)
}

func (p *BleveRecallProjection) activateGeneration(ctx context.Context, scope memory.Scope, id string, scoped bool) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if p == nil || !validBleveGenerationID(id) {
		return memory.PermanentError(memory.CodeInvalidDerived, "bleve activation is invalid", nil)
	}
	p.mu.RLock()
	root := p.root
	p.mu.RUnlock()
	index, err := openCommittedBleveGeneration(root, id)
	if err != nil {
		return memory.PermanentError(memory.CodeConflict, "bleve generation is not durably committed", err)
	}
	markerPath := filepath.Join(root, bleveActiveGenerationFile)
	scopeKey := ""
	if scoped {
		scopeKey = projectionScopeKey(scope)
		markerPath = filepath.Join(root, bleveScopedActivePrefix+scopeKey)
	}
	temporaryPath := markerPath + ".tmp." + id
	p.mu.Lock()
	if err := os.WriteFile(temporaryPath, []byte(id+"\n"), 0o600); err != nil {
		p.mu.Unlock()
		_ = index.Close()
		return fmt.Errorf("write bleve active marker: %w", err)
	}
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		p.mu.Unlock()
		_ = os.Remove(temporaryPath)
		_ = index.Close()
		return fmt.Errorf("activate bleve generation: %w", err)
	}
	var previous bleve.Index
	if scoped {
		previous = p.activeByScope[scopeKey]
		p.activeByScope[scopeKey] = index
		p.activeScopeIDs[scopeKey] = id
	} else {
		previous = p.active
		p.active = index
		p.activeID = id
	}
	p.mu.Unlock()
	if previous != nil {
		return previous.Close()
	}
	return nil
}

// Close releases every active scope index and the legacy process-wide index.
func (p *BleveRecallProjection) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
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
	p.mu.Unlock()

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

func projectionScopeKey(scope memory.Scope) string {
	digest := sha256.Sum256([]byte(scope.Key + "\x00" + string(scope.Kind)))
	return hex.EncodeToString(digest[:])
}
