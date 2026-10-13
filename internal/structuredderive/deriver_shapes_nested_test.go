package structuredderive_test

import (
	"encoding/json"
	"fmt"
	"strings"
)

// These mutations specify the accepted wire contract independently of its
// production shape tree. They catch missing validators at every object depth.
func nestedContractCases(stage, candidate string, wrap func(string) string) []contractCase {
	var cases []contractCase

	add := func(name, raw string, valid bool) { cases = append(cases, contractCase{name, wrap(raw), valid, false}) }
	replace := func(name, old, replacement string, valid bool) {
		add(name, strings.Replace(candidate, old, replacement, 1), valid)
	}

	switch stage {
	case "atoms":
		atomContractCases(add, replace)
	case "canonical_semantics":
		semanticContractCases(add, replace)
	case "scenarios":
		scenarioContractCases(add, replace)
	case "profile":
		profileContractCases(add, replace)
	}

	return append(cases, objectContractCases(candidate, wrap)...)
}

func atomContractCases(add func(string, string, bool), replace func(string, string, string, bool)) {
	replace("category_enum", `"fact"`, `"fiction"`, false)
	replace("relation_enum", `"new"`, `"related"`, false)
	replace("new_null_target", `"relation":"new"`, `"relation":"new","target":null`, true)
	replace("new_target", `"relation":"new"`, `"relation":"new","target":`+refFixture, false)

	for _, relation := range []string{"coexist", "supersede"} {
		replace(relation+"_missing_target", `"new"`, `"`+relation+`"`, false)
		replace(relation+"_null_target", `"relation":"new"`, `"relation":"`+relation+`","target":null`, false)
		replace(relation+"_target", `"relation":"new"`, `"relation":"`+relation+`","target":`+refFixture, true)
	}
}

func semanticContractCases(add func(string, string, bool), replace func(string, string, string, bool)) {
	for _, entry := range []struct{ name, old, replacement string }{
		{"Kind_enum", `"Kind":"state"`, `"Kind":"unknown"`},
		{"kind_enum", `"kind":"state"`, `"kind":"unknown"`},
		{"role_enum", `"role":"user"`, `"role":"system"`},
		{"assertion_enum", `"assertion_mode":"user"`, `"assertion_mode":"assistant"`},
		{"sensitivity_enum", `"sensitivity":"standard"`, `"sensitivity":"public"`},
		{"retention_enum", `"retention":"standard"`, `"retention":"forever"`},
		{"timestamp_type", `"2026-08-03T16:00:00Z"`, `1`},
		{"timestamp_format", `"2026-08-03T16:00:00Z"`, `"yesterday"`},
		{"evidence_null_element", `"evidence":[{`, `"evidence":[null,{`},
		{"Qualifiers_type", `"Kind":"state"`, `"Qualifiers":{},"Kind":"state"`},
		{"Qualifiers_null_element", `"Kind":"state"`, `"Qualifiers":[null],"Kind":"state"`},
	} {
		replace(entry.name, entry.old, entry.replacement, false)
	}

	scalarSemanticCases(replace)

	replace("qualifiers_null", `"Kind":"state"`, `"Qualifiers":null,"Kind":"state"`, true)
	replace("qualifiers_strings", `"Kind":"state"`, `"Qualifiers":["historical"],"Kind":"state"`, true)

	for _, field := range []string{"valid_from",
		"valid_until",
		"event_at",
		"expires_at",
		"temporal_text",
		"temporal_precision",
		"timezone"} {
		replace(field+"_null", `"observed_at":`, `"`+field+`":null,"observed_at":`, true)

		value := `"2026-08-03T16:00:00Z"`
		if strings.HasPrefix(field, "temporal_") || field == "timezone" {
			value = `"UTC"`
		}

		replace(field+"_string", `"observed_at":`, `"`+field+`":`+value+`,"observed_at":`, true)
		replace(field+"_wrong_type", `"observed_at":`, `"`+field+`":[],"observed_at":`, false)
	}

	replace("digest_null", `"source_id":`, `"text_digest":null,"source_id":`, true)
	replace("digest_type", `"source_id":`, `"text_digest":{},"source_id":`, false)
}

func scenarioContractCases(add func(string, string, bool), replace func(string, string, string, bool)) {
	replace("supersedes_null", `"topic_key":`, `"supersedes":null,"topic_key":`, true)
	replace("supersedes_ref", `"topic_key":`, `"supersedes":`+refFixture+`,"topic_key":`, true)
	replace("atoms_null_element", `"atoms":[{`, `"atoms":[null,{`, false)
}

func profileContractCases(add func(string, string, bool), replace func(string, string, string, bool)) {
	replace("disposition_enum", `"upsert"`, `"create"`, false)
	add("upsert_no_parent", `{"disposition":"upsert","summary":"Bishkek"}`, false)
	add("upsert_empty_parents", `{"disposition":"upsert","summary":"Bishkek","atoms":[],"scenarios":null}`, false)
	add("upsert_scenarios",
		`{"disposition":"upsert","summary":"Bishkek","atoms":null,"scenarios":[`+refFixture+`],"supersedes":null}`,
		true)
	add("upsert_empty_summary", `{"disposition":"upsert","summary":"","atoms":[`+refFixture+`]}`, false)
	add("skip_nulls", `{"disposition":"skip","summary":null,"atoms":null,"scenarios":null,"supersedes":null}`, true)
	add("skip_empty", `{"disposition":"skip","summary":"","atoms":[],"scenarios":[]}`, true)
	replace("skip_populated", `"upsert"`, `"skip"`, false)

	for _, field := range []string{"atoms", "scenarios", "supersedes"} {
		value := refFixture
		if field != "supersedes" {
			value = `[` + refFixture + `]`
		}

		add("skip_"+field, `{"disposition":"skip","`+field+`":`+value+`}`, false)
	}
}

func objectContractCases(candidate string, wrap func(string) string) []contractCase {
	var cases []contractCase

	// Probe every object represented by the populated fixture, including evidence,
	// temporal, memory and all revision references, without consulting wireShape.
	var root any
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		panic(err)
	}

	var visit func(any, []any)

	visit = func(value any, path []any) {
		switch node := value.(type) {
		case map[string]any:
			cases = append(cases, objectLexicalCases(candidate, path, wrap)...)
			cases = append(cases, objectMemberCases(candidate, path, node, wrap)...)

			for key, child := range node {
				visit(child, append(append([]any{}, path...), key))
			}
		case []any:
			for i, child := range node {
				visit(child, append(append([]any{}, path...), i))
			}
		}
	}
	visit(root, nil)

	return cases
}
func pathName(path []any) string {
	raw, _ := json.Marshal(path)

	return string(raw)
}
func mutateObject(candidate string, path []any, mutate func(map[string]any)) string {
	var root any
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		panic(err)
	}

	object := root

	for _, part := range path {
		switch key := part.(type) {
		case string:
			object = object.(map[string]any)[key]
		case int:
			object = object.([]any)[key]
		}
	}

	mutate(object.(map[string]any))

	raw, _ := json.Marshal(root)

	return string(raw)
}
func duplicateObject(candidate string, path []any, escaped bool) string {
	// Inject a unique placeholder into exactly one object, then replace it with
	// an existing name. This preserves duplicate tokens through serialization.
	var key string

	marked := mutateObject(candidate, path, func(object map[string]any) {
		key = firstObjectKey(object)
		object["ZZ_DUPLICATE_MARKER"] = nil
	})

	encoded, _ := json.Marshal(key)
	if escaped {
		encoded = []byte(fmt.Sprintf(`"\u%04x%s"`, key[0], key[1:]))
	}

	return strings.Replace(marked, `"ZZ_DUPLICATE_MARKER":null`, string(encoded)+`:null`, 1)
}

func firstObjectKey(object map[string]any) string {
	var first string
	for key := range object {
		if first == "" || key < first {
			first = key
		}
	}

	return first
}

func objectLexicalCases(candidate string, path []any, wrap func(string) string) []contractCase {
	unknown := mutateObject(candidate, path, func(object map[string]any) { object["unexpected"] = true })
	wrongCase := mutateObject(candidate, path, func(object map[string]any) {
		key := firstObjectKey(object)
		value := object[key]
		delete(object, key)
		object[strings.ToUpper(key)] = value
	})
	prefix := "nested_" + pathName(path)

	return []contractCase{
		{prefix + "_unknown", wrap(unknown), false, false},
		{prefix + "_case", wrap(wrongCase), false, false},
		{prefix + "_duplicate", wrap(duplicateObject(candidate, path, false)), false, true},
		{prefix + "_escaped_duplicate", wrap(duplicateObject(candidate, path, true)), false, true},
	}
}

func objectMemberCases(candidate string, path []any, node map[string]any, wrap func(string) string) []contractCase {
	var cases []contractCase
	// Fixture members are required; profile atoms are this fixture's only parents.
	for key := range node {
		for _, mutation := range []string{"missing", "null", "wrong_type"} {
			raw := mutateObject(candidate, path, func(object map[string]any) {
				switch mutation {
				case "missing":
					delete(object, key)
				case "null":
					object[key] = nil
				case "wrong_type":
					object[key] = true
				}
			})
			name := "nested_" + pathName(path) + "_" + key + "_" + mutation
			cases = append(cases, contractCase{name, wrap(raw), false, false})
		}
	}

	return cases
}

func scalarSemanticCases(replace func(string, string, string, bool)) {
	for _, timestamp := range []string{"2026-08-03T16:00:00+24:00",
		"2026-08-03T16:00:00+01:60",
		"2026-08-03T16:00:00,1Z",
		"2026-08-03T1:00:00Z",
		"2026-08-03t16:00:00z",
		"2026-12-31T23:59:60Z",
		"2026-02-30T16:00:00Z"} {
		replace("invalid_timestamp_"+timestamp, `"2026-08-03T16:00:00Z"`, `"`+timestamp+`"`, false)
	}

	for _, timestamp := range []string{"2026-08-03T16:00:00.123456789Z",
		"2026-08-03T16:00:00+06:30",
		"2026-08-03T16:00:00-01:00"} {
		replace("valid_timestamp_"+timestamp, `"2026-08-03T16:00:00Z"`, `"`+timestamp+`"`, true)
	}

	for _, number := range []string{"-1", "4294967296", "0.5", "1e100", "1e-10000", "true", `"1"`} {
		replace("start_byte_"+number, `"start_byte":0`, `"start_byte":`+number, false)
	}

	for _, number := range []string{"4294967295", "1.0", "1e0", "-0", "0e10000"} {
		replace("start_byte_"+number, `"start_byte":0`, `"start_byte":`+number, true)
	}
}
