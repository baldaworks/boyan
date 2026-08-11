package canonicalcore

import memory "github.com/baldaworks/boyan/pkg/memory"

func cloneTurn(turn memory.Turn) memory.Turn {
	turn.Messages = append([]memory.Message(nil), turn.Messages...)
	return turn
}
