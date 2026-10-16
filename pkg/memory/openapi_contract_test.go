package memory_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

func openAPIDocument(t *testing.T) map[string]any {
	t.Helper()

	data, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	return document
}

func openAPIValue(t *testing.T, value any, path ...string) any {
	t.Helper()

	for _, key := range path {
		mapping, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected mapping before %q", key)
		}

		value, ok = mapping[key]
		if !ok {
			t.Fatalf("missing %q", key)
		}
	}

	return value
}

func openAPIJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestOpenAPIIngestExamplesPassDomainValidation(t *testing.T) {
	document := openAPIDocument(t)
	turnJSON := openAPIJSON(t, openAPIValue(t, document, "paths", "/v1/ingest/turns", "post",
		"requestBody", "content", "application/json", "examples", "terminalTurn", "value"))
	boundaryJSON := openAPIJSON(t, openAPIValue(t, document, "paths", "/v1/ingest/boundaries", "post",
		"requestBody", "content", "application/json", "examples", "boundary", "value"))

	var turn memory.IngestEnvelope[memory.Turn]

	var boundary memory.IngestEnvelope[memory.Boundary]

	if err := json.Unmarshal(turnJSON, &turn); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(boundaryJSON, &boundary); err != nil {
		t.Fatal(err)
	}

	if err := turn.Validate(memory.Turn.Validate); err != nil {
		t.Errorf("documented turn cannot be ingested: %v", err)
	}

	if err := boundary.Validate(memory.Boundary.Validate); err != nil {
		t.Errorf("documented boundary cannot be ingested: %v", err)
	}
}

func openAPITurnSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(openAPIJSON(t, openAPIDocument(t))))
	if err != nil {
		t.Fatal(err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:boyan:openapi", document); err != nil {
		t.Fatal(err)
	}

	schema, err := compiler.Compile("urn:boyan:openapi#/components/schemas/Turn")
	if err != nil {
		t.Fatal(err)
	}

	return schema
}

func TestOpenAPITurnSequenceMatchesDomain(t *testing.T) {
	schema := openAPITurnSchema(t)

	turn, err := memory.NewTurn(memory.Scope{Key: "slack:C123", Kind: memory.ScopeKindGroup},
		memory.ConversationRef{ConversationID: "conv-1", AgentConversationID: "agent-conv-1"},
		"source-turn-1", time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC), "hello", "hi")
	if err != nil {
		t.Fatal(err)
	}

	tool := memory.Message{Role: memory.MessageRoleTool, Text: "result", ToolName: "search", ToolCallID: "call-1",
		MessageID: memory.TurnToolMessageID(turn.ExportID, "search", "call-1")}

	cases := []struct {
		name   string
		status memory.TurnTerminalStatus
		lead   []memory.Message
		tools  int
		valid  bool
	}{
		{"default success", "", turn.Messages, 0, true},
		{"success with tools", memory.TurnTerminalStatusSuccess, turn.Messages, 16, true},
		{"success needs assistant", "", turn.Messages[:1], 0, false},
		{"explicit success needs assistant", memory.TurnTerminalStatusSuccess, turn.Messages[:1], 1, false},
		{"failed user only", memory.TurnTerminalStatusFailed, turn.Messages[:1], 0, true},
		{"interrupted user and tools", memory.TurnTerminalStatusInterrupted, turn.Messages[:1], 16, true},
		{"failed with assistant", memory.TurnTerminalStatusFailed, turn.Messages, 16, true},
		{"too many tools with assistant", memory.TurnTerminalStatusFailed, turn.Messages, 17, false},
		{"too many tools without assistant", memory.TurnTerminalStatusFailed, turn.Messages[:1], 17, false},
		{"assistant first", "", []memory.Message{turn.Messages[1], turn.Messages[0]}, 0, false},
		{"repeated assistant", "", []memory.Message{turn.Messages[0], turn.Messages[1], turn.Messages[1]}, 0, false},
		{"user with tool identity", "", []memory.Message{
			{Role: memory.MessageRoleUser, Text: "hello", ToolName: "search"}, turn.Messages[1]}, 0, false},
		{"tool missing identity", memory.TurnTerminalStatusFailed, []memory.Message{
			turn.Messages[0], {Role: memory.MessageRoleTool, Text: "result"}}, 0, false},
		{"noncanonical tool name", memory.TurnTerminalStatusFailed, []memory.Message{
			turn.Messages[0], {Role: memory.MessageRoleTool, Text: "result", ToolName: " search",
				ToolCallID: "call-1", MessageID: tool.MessageID}}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := turn
			candidate.TerminalStatus = tc.status
			candidate.Messages = append([]memory.Message(nil), tc.lead...)

			for i := range tc.tools {
				message := tool
				message.ToolCallID = fmt.Sprintf("call-%d", i)
				message.MessageID = memory.TurnToolMessageID(turn.ExportID, message.ToolName, message.ToolCallID)
				candidate.Messages = append(candidate.Messages, message)
			}

			checkOpenAPITurn(t, schema, candidate, tc.valid)
		})
	}
}

func checkOpenAPITurn(t *testing.T, schema *jsonschema.Schema, candidate memory.Turn, valid bool) {
	t.Helper()

	if err := candidate.Validate(); (err == nil) != valid {
		t.Fatalf("domain accepted=%v, want %v: %v", err == nil, valid, err)
	}

	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(openAPIJSON(t, candidate)))
	if err != nil {
		t.Fatal(err)
	}

	if err := schema.Validate(value); (err == nil) != valid {
		t.Errorf("schema accepted=%v, want %v: %v", err == nil, valid, err)
	}
}

func TestOpenAPIMessageUnicodeWhitespaceMatchesDomain(t *testing.T) {
	schema := openAPITurnSchema(t)

	turn, err := memory.NewTurn(memory.Scope{Key: "slack:C123", Kind: memory.ScopeKindGroup},
		memory.ConversationRef{ConversationID: "conv-1", AgentConversationID: "agent-conv-1"},
		"source-turn-1", time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC), "hello", "hi")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{"NEXT LINE", "\u0085", false},
		{"NO-BREAK SPACE", "\u00a0", false},
		{"BOM is content", "\ufeff", true},
	} {
		for _, field := range []string{"text", "tool_name", "tool_call_id"} {
			for _, edge := range []string{"leading", "trailing"} {
				t.Run(tc.name+"/"+field+"/"+edge, func(t *testing.T) {
					candidate := turn
					candidate.Messages = append([]memory.Message(nil), turn.Messages...)
					message := memory.Message{Role: memory.MessageRoleTool, Text: "result",
						ToolName: "search", ToolCallID: "call-1"}

					value := tc.value + "search"
					if edge == "trailing" {
						value = "search" + tc.value
					}

					switch field {
					case "text":
						candidate.Messages[0].Text = tc.value
					case "tool_name":
						message.ToolName = value
					case "tool_call_id":
						message.ToolCallID = value
					}

					message.MessageID = memory.TurnToolMessageID(turn.ExportID, message.ToolName, message.ToolCallID)
					candidate.Messages = append(candidate.Messages, message)
					checkOpenAPITurn(t, schema, candidate, tc.valid)
				})
			}
		}
	}
}
