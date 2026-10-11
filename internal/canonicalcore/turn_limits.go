package canonicalcore

import memory "github.com/baldaworks/boyan/pkg/memory"

func turnTextExceeds(turn memory.Turn, limit int) bool {
	remaining := limit
	for _, message := range turn.Messages {
		if len(message.Text) > remaining {
			return true
		}

		remaining -= len(message.Text)
	}

	return false
}
