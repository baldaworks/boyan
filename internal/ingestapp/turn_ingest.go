package ingestapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// IngestTurn implements TurnIngestor.
func (p *TurnProcessor) IngestTurn(ctx context.Context, turn memory.Turn) error {
	_, err := p.ProcessTurn(ctx, turn)
	return err
}

var _ TurnIngestor = (*TurnProcessor)(nil)
