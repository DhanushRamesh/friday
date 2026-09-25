package tool

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Validate : Checks the arguments a model produced against the schema, and
// says what is wrong in words the model can act on.
//
// The message matters more than the refusal. A model told "invalid arguments"
// guesses again; a model told which argument, what was wrong with it and what
// was allowed corrects itself on the next hop. That correction costs one
// round trip and turns a wrong answer into a right one, which is why an
// invalid call is answered rather than failed.
func Validate(s Schema, args json.RawMessage) error {
	given := map[string]any{}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &given); err != nil {
			return fmt.Errorf("the arguments were not a JSON object: %s", err)
		}
	}

	var wrong []string

	for _, name := range s.Required {
		if _, ok := given[name]; !ok {
			wrong = append(wrong, fmt.Sprintf("%s is required and was not given", name))
		}
	}

	for name, value := range given {
		p, known := s.Properties[name]
		if !known {
			wrong = append(wrong, fmt.Sprintf(
				"%s is not an argument of this tool; it takes %s", name, listed(s)))
			continue
		}
		if why := check(name, p, value); why != "" {
			wrong = append(wrong, why)
		}
	}

	if len(wrong) == 0 {
		return nil
	}
	sort.Strings(wrong)
	return fmt.Errorf("%s", strings.Join(wrong, "; "))
}

// check : What is wrong with one argument, or empty when nothing is.
func check(name string, p Property, value any) string {
	switch p.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%s must be text, and was %s", name, kindOf(value))
		}
		if len(p.Enum) > 0 && !among(text, p.Enum) {
			return fmt.Sprintf("%s was %q, and must be one of: %s",
				name, text, strings.Join(p.Enum, ", "))
		}
		if p.Pattern != "" {
			if ok, err := regexp.MatchString(p.Pattern, text); err == nil && !ok {
				return fmt.Sprintf("%s was %q, which is not the right shape", name, text)
			}
		}

	case "integer", "number":
		// Every JSON number decodes as a float, so a whole number is checked
		// rather than assumed from the type.
		n, ok := value.(float64)
		if !ok {
			return fmt.Sprintf("%s must be a number, and was %s", name, kindOf(value))
		}
		if p.Type == "integer" && n != float64(int(n)) {
			return fmt.Sprintf("%s must be a whole number, and was %v", name, n)
		}
		if p.Minimum != nil && int(n) < *p.Minimum {
			return fmt.Sprintf("%s was %v, and the least allowed is %d", name, n, *p.Minimum)
		}
		if p.Maximum != nil && int(n) > *p.Maximum {
			return fmt.Sprintf("%s was %v, and the most allowed is %d", name, n, *p.Maximum)
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Sprintf("%s must be true or false, and was %s", name, kindOf(value))
		}
	}

	return ""
}

// listed : The arguments a tool takes, named, for a model that used one it
// does not.
func listed(s Schema) string {
	if len(s.Properties) == 0 {
		return "none"
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// among : Whether text is one of the allowed values.
func among(text string, allowed []string) bool {
	for _, a := range allowed {
		if a == text {
			return true
		}
	}
	return false
}

// kindOf : What a value is, in words, so a correction says what was wrong
// rather than only that something was.
func kindOf(value any) string {
	switch value.(type) {
	case string:
		return "text"
	case float64:
		return "a number"
	case bool:
		return "true or false"
	case nil:
		return "nothing"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return "something else"
}
