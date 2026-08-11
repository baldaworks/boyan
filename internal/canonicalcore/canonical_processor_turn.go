package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func prepareCanonicalTurn(turn Turn) (Turn, error) {
	if err := turn.Validate(); err != nil {
		return Turn{}, err
	}
	turn = cloneTurn(turn)
	expectedSourceID := TurnSourceID(turn.ExportID)
	if turn.SourceID != "" && turn.SourceID != expectedSourceID {
		return Turn{}, PermanentError(CodePermanent, "canonical turn source identity does not match export", nil)
	}
	turn.SourceID = expectedSourceID
	assignCanonicalMessageIDs(turn.Messages, turn.ExportID)
	return turn, nil
}

func assignCanonicalMessageIDs(messages []Message, turnExportID string) {
	for index := range messages {
		if messages[index].MessageID != "" {
			continue
		}
		if messages[index].Role == MessageRoleTool {
			messages[index].MessageID = TurnToolMessageID(turnExportID, messages[index].ToolName, messages[index].ToolCallID)
			continue
		}
		messages[index].MessageID = TurnMessageID(turnExportID, messages[index].Role)
	}
}
