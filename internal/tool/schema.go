package tool

import "encoding/json"

// Schema : What a tool takes.
//
// A typed structure rather than raw JSON, so that a malformed schema cannot
// be written and so that a test can walk it. A description missing from a
// property is the commonest reason a model passes the wrong argument, and it
// is only findable if the schema is something other than a string.
type Schema struct {
	// Properties : The arguments, by name.
	Properties map[string]Property
	// Required : Which of them must be given.
	Required []string
}

// Property : One argument.
type Property struct {
	// Type : One of string, integer, number, boolean.
	Type string
	// Description : What it is, written for the model that has to fill it
	// in. Required: an undescribed argument is guessed at.
	Description string
	// Enum : The only values allowed. Given wherever the set is knowable,
	// because a model that must choose from a list cannot invent a value.
	Enum []string
	// Pattern : A regular expression the value must match. For an identifier
	// with a shape, so that a made-up one is refused before it is used.
	Pattern string
	// Minimum, Maximum : Bounds for a number. Nil for no bound.
	Minimum *int
	Maximum *int
	// Default : What is used when it is not given. Nil for none.
	Default any
}

// MarshalJSON : Renders the schema as the JSON Schema a service expects.
func (s Schema) MarshalJSON() ([]byte, error) {
	properties := make(map[string]any, len(s.Properties))
	for name, p := range s.Properties {
		properties[name] = p.asMap()
	}

	// Required is always present, as an empty array rather than absent: a
	// missing key reads as unknown, and every argument being optional is a
	// thing worth stating.
	required := s.Required
	if required == nil {
		required = []string{}
	}

	return json.Marshal(map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	})
}

// asMap : The property as JSON Schema keys, leaving out what was not set.
func (p Property) asMap() map[string]any {
	out := map[string]any{
		"type":        p.Type,
		"description": p.Description,
	}
	if len(p.Enum) > 0 {
		out["enum"] = p.Enum
	}
	if p.Pattern != "" {
		out["pattern"] = p.Pattern
	}
	if p.Minimum != nil {
		out["minimum"] = *p.Minimum
	}
	if p.Maximum != nil {
		out["maximum"] = *p.Maximum
	}
	if p.Default != nil {
		out["default"] = p.Default
	}
	return out
}

// Bound : A pointer to n, for Minimum and Maximum.
func Bound(n int) *int { return &n }
