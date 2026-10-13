package structuredderive

import "encoding/json"

// wireShape describes only the fixed candidate contracts below. It is also the
// provider schema, so adding a wire member cannot bypass runtime validation.
const timestampPattern = `^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T` +
	`([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`

type wireShape struct {
	Pattern              string               `json:"pattern,omitempty"`
	Type                 string               `json:"type,omitempty"`
	Properties           map[string]wireShape `json:"properties,omitempty"`
	Required             []string             `json:"required,omitempty"`
	AdditionalProperties *bool                `json:"additionalProperties,omitempty"`
	Items                *wireShape           `json:"items,omitempty"`
	Enum                 []any                `json:"enum,omitempty"`
	AnyOf                []wireShape          `json:"anyOf,omitempty"`
	Format               string               `json:"format,omitempty"`
	Minimum              *uint64              `json:"minimum,omitempty"`
	Maximum              *uint64              `json:"maximum,omitempty"`
	MinItems             *int                 `json:"minItems,omitempty"`
	MaxItems             *int                 `json:"maxItems,omitempty"`
	MinLength            *int                 `json:"minLength,omitempty"`
}

// wireShape describes only the fixed four candidate contracts below.
// Runtime validation and provider schemas share this explicit tree.
func objectShape(properties map[string]wireShape, required ...string) wireShape {
	additional := false

	return wireShape{Type: "object", Properties: properties, Required: required, AdditionalProperties: &additional}
}

func stringShape(values ...string) wireShape {
	s := wireShape{Type: "string"}

	for _, v := range values {
		s.Enum = append(s.Enum, v)
	}

	return s
}

func nullableShape(s wireShape) wireShape { return wireShape{AnyOf: []wireShape{s, {Type: "null"}}} }
func arrayShape(item wireShape) wireShape { return wireShape{Type: "array", Items: &item} }
func refShape() wireShape {
	return objectShape(map[string]wireShape{
		"item_id": stringShape(), "revision_id": stringShape(),
	}, "item_id", "revision_id")
}

func atomsShape() wireShape {
	fields := func(relation wireShape, target wireShape) map[string]wireShape {
		return map[string]wireShape{
			"category": stringShape("fact", "preference", "constraint", "decision", "event"),
			"text":     stringShape(),
			"relation": relation,
			"target":   target}
	}

	return wireShape{AnyOf: []wireShape{
		objectShape(fields(stringShape("new"), wireShape{Type: "null"}), "category", "text", "relation"),
		objectShape(fields(stringShape("coexist", "supersede"), refShape()), "category", "text", "relation", "target"),
	}}
}

func temporalShape() wireShape {
	timestamp := wireShape{Type: "string", Format: "date-time", Pattern: timestampPattern}

	return objectShape(map[string]wireShape{
		"observed_at": timestamp, "valid_from": nullableShape(timestamp), "valid_until": nullableShape(timestamp),
		"event_at": nullableShape(timestamp), "expires_at": nullableShape(timestamp),
		"temporal_text":      nullableShape(stringShape()),
		"temporal_precision": nullableShape(stringShape()),
		"timezone":           nullableShape(stringShape()),
	}, "observed_at")
}

func evidenceShape() wireShape {
	var minimum uint64

	maximum := uint64(1<<32 - 1)
	integer := wireShape{Type: "integer", Minimum: &minimum, Maximum: &maximum}

	return objectShape(map[string]wireShape{
		"source_id": stringShape(), "message_id": stringShape(), "role": stringShape("user", "assistant", "tool"),
		"start_byte":     integer,
		"end_byte":       integer,
		"assertion_mode": stringShape("user", "trusted_tool"),
		"text_digest":    nullableShape(stringShape()),
	}, "source_id", "message_id", "role", "start_byte", "end_byte", "assertion_mode")
}

func semanticsShape() wireShape {
	kind := stringShape("state", "event")
	memory := objectShape(map[string]wireShape{
		"kind": kind, "statement": stringShape(), "temporal": temporalShape(), "evidence": arrayShape(evidenceShape()),
		"sensitivity": stringShape("standard", "sensitive"), "retention": stringShape("standard", "ephemeral"),
	}, "kind", "statement", "temporal", "evidence", "sensitivity", "retention")

	return objectShape(map[string]wireShape{
		"Kind":       kind,
		"Subject":    stringShape(),
		"Predicate":  stringShape(),
		"Qualifiers": nullableShape(arrayShape(stringShape())),
		"Memory":     memory,
	}, "Kind", "Subject", "Predicate", "Memory")
}

func scenariosShape() wireShape {
	return objectShape(map[string]wireShape{
		"topic_key":  stringShape(),
		"title":      stringShape(),
		"summary":    stringShape(),
		"atoms":      arrayShape(refShape()),
		"supersedes": nullableShape(refShape()),
	}, "topic_key", "title", "summary", "atoms")
}

func profileShape() wireShape {
	zero, one := 0, 1
	emptyRefs := arrayShape(refShape())
	emptyRefs.MaxItems = &zero
	skip := objectShape(map[string]wireShape{
		"disposition": stringShape("skip"), "summary": nullableShape(stringShape("")),
		"atoms": nullableShape(emptyRefs), "scenarios": nullableShape(emptyRefs), "supersedes": {Type: "null"},
	}, "disposition")
	upsert := func(parent string) wireShape {
		summary := stringShape()
		summary.MinLength = &one
		parents := arrayShape(refShape())
		parents.MinItems = &one
		fields := map[string]wireShape{"disposition": stringShape("upsert"), "summary": summary,
			"atoms":      nullableShape(arrayShape(refShape())),
			"scenarios":  nullableShape(arrayShape(refShape())),
			"supersedes": nullableShape(refShape())}
		fields[parent] = parents

		return objectShape(fields, "disposition", "summary", parent)
	}

	return wireShape{AnyOf: []wireShape{skip, upsert("atoms"), upsert("scenarios")}}
}

func stageShape(stage string) wireShape {
	var output wireShape

	switch stage {
	case "atoms":
		output = arrayShape(atomsShape())
	case "canonical_semantics":
		output = arrayShape(semanticsShape())
	case "scenarios":
		output = arrayShape(scenariosShape())
	case "profile":
		output = profileShape()
	default:
		return wireShape{}
	}

	return objectShape(map[string]wireShape{"output": output}, "output")
}

func outputSchema(stage string) string {
	// This fixed tree contains only JSON-compatible primitives.
	raw, _ := json.Marshal(stageShape(stage))

	return string(raw)
}
