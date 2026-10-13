package structuredderive_test

import (
	"strings"
	"testing"

	derive "github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const semanticFixture = `{"Kind":"state","Subject":"Ada","Predicate":"lives_in","Memory":{` +
	`"kind":"state","statement":"Ada lives in Bishkek","temporal":{"observed_at":"2026-08-` +
	`03T16:00:00Z"},` +
	`"evidence":[{"source_id":"s","message_id":"m","role":"user","start_byte":0,"end_byte"` +
	`:3,` +
	`"assertion_mode":"user"}],"sensitivity":"standard","retention":"standard"}}`
const refFixture = `{"item_id":"i","revision_id":"r"}`

type contractCase struct {
	name, raw      string
	valid, lexical bool
}

func TestStructuredContracts(t *testing.T) {
	t.Parallel()

	for _, adapter := range adapterContracts(t) {
		t.Run(adapter.name, func(t *testing.T) {
			t.Parallel()

			candidate := contractCandidate(adapter.name)
			wrap := func(candidate string) string {
				if adapter.name == "profile" {
					return `{"output":` + candidate + `}`
				}

				return `{"output":[` + candidate + `]}`
			}
			schema := advertisedSchema(t, adapter)
			cases := envelopeContractCases(wrap, candidate)
			cases = append(cases, contractCase{"empty", adapter.empty, true, false})

			stage := adapter.name
			if stage == "canonical" {
				stage = "canonical_semantics"
			}

			cases = append(cases, nestedContractCases(stage, candidate, wrap)...)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) { checkContractCase(t, adapter, schema, tc) })
			}
		})
	}
}

func contractCandidate(stage string) string {
	switch stage {
	case "atoms":
		return `{"category":"fact","text":"Ada lives in Bishkek","relation":"new"}`
	case "canonical":
		return semanticFixture
	case "scenarios":
		return `{"topic_key":"home","title":"Home","summary":"Bishkek","atoms":[` + refFixture + `]}`
	case "profile":
		return `{"disposition":"upsert","summary":"Bishkek","atoms":[` + refFixture + `]}`
	default:
		panic("unknown contract")
	}
}

func envelopeContractCases(wrap func(string) string, candidate string) []contractCase {
	valid := wrap(candidate)

	return []contractCase{
		{"populated", valid, true, false}, {"missing_output", `{}`, false, false}, {"null_envelope", `null`, false, false},
		{"null_output", `{"output":null}`, false, false},
		{"wrong_case", strings.Replace(valid, `"output"`, `"Output"`, 1), false, false},
		{"unknown_envelope", strings.Replace(valid, `{"output":`, `{"secret":"private-token","output":`, 1), false, false},
		{"duplicate_output", strings.TrimSuffix(valid, "}") + `,"output":null}`, false, true},
		{"escaped_duplicate", strings.TrimSuffix(valid, "}") + `,"\u006futput":null}`, false, true},
		{"trailing_value", valid + ` {}`, false, true}, {"wrong_container", `{"output":"private-token"}`, false, false},
		{"empty_candidate", wrap(`{}`), false, false}, {"null_candidate", wrap(`null`), false, false},
		{"unknown_candidate", wrap(strings.Replace(candidate, `{`, `{"secret":"private-token",`, 1)), false, false},
	}
}

func advertisedSchema(t *testing.T, adapter adapterContract) *jsonschema.Schema {
	t.Helper()

	invoker := &contractInvoker{raw: []byte(adapter.empty)}
	deriver, err := derive.NewDeriver(invoker)
	requireNoError(t, err, "deriver: %v")
	requireNoError(t, adapter.call(deriver), "empty fixture: %v")

	document, err := jsonschema.UnmarshalJSON(strings.NewReader(invoker.last.OutputSchema))
	requireNoError(t, err, "advertised schema: %v")

	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	requireNoError(t, compiler.AddResource("urn:boyan:test", document), "schema resource: %v")
	schema, err := compiler.Compile("urn:boyan:test")
	requireNoError(t, err, "compile schema: %v")

	return schema
}

func checkContractCase(t *testing.T, adapter adapterContract, schema *jsonschema.Schema, tc contractCase) {
	t.Helper()

	deriver, err := derive.NewDeriver(&contractInvoker{raw: []byte(tc.raw)})
	requireNoError(t, err, "deriver: %v")

	err = adapter.call(deriver)
	if (err == nil) != tc.valid {
		t.Errorf("runtime accepted=%v, want %v: %v", err == nil, tc.valid, err)
	}

	if err != nil {
		requireContractError(t, err, memory.CodeInvalidDerived, false)

		if strings.Contains(err.Error(), "private-token") {
			t.Error("raw response leaked")
		}
	}

	if tc.lexical {
		return
	}

	value, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.raw))
	requireNoError(t, err, "fixture: %v")

	if got := schema.Validate(value) == nil; got != tc.valid {
		t.Errorf("schema accepted=%v, want %v", got, tc.valid)
	}
}
