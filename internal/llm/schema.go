package llm

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"google.golang.org/genai"
)

var ErrUnsupportedType = errors.New("type can't be used as a response schema")

// Build a response schema from the type v points to, so a model's JSON
// output can be unmarshalled into it. Struct fields use their encoding/json
// names, and are required unless tagged omitempty. A description tag is
// shown to the model, and an enum tag limits a string field to a comma
// separated list of values:
//
//	type Decision struct {
//		Reason string   `json:"reason" description:"why this action was chosen"`
//		Action string   `json:"action" enum:"read,answer"`
//		Paths  []string `json:"paths,omitempty"`
//	}
//
// The model fills in fields in the order they are declared, so fields the
// model should think through first go first.
func SchemaFor(v any) (*genai.Schema, error) {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("%w: expected a pointer, got %v", ErrUnsupportedType, t)
	}
	return schemaForType(t.Elem(), map[reflect.Type]bool{})
}

// in_progress holds the structs being built, to catch recursive types
func schemaForType(t reflect.Type, in_progress map[reflect.Type]bool) (*genai.Schema, error) {
	switch t.Kind() {
	case reflect.Pointer:
		schema, err := schemaForType(t.Elem(), in_progress)
		if err != nil {
			return nil, err
		}
		schema.Nullable = genai.Ptr(true)
		return schema, nil
	case reflect.String:
		return &genai.Schema{Type: genai.TypeString}, nil
	case reflect.Bool:
		return &genai.Schema{Type: genai.TypeBoolean}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &genai.Schema{Type: genai.TypeInteger}, nil
	case reflect.Float32, reflect.Float64:
		return &genai.Schema{Type: genai.TypeNumber}, nil
	case reflect.Slice, reflect.Array:
		// encoding/json writes byte slices as base64 strings
		if t.Elem().Kind() == reflect.Uint8 {
			return &genai.Schema{Type: genai.TypeString}, nil
		}
		items, err := schemaForType(t.Elem(), in_progress)
		if err != nil {
			return nil, err
		}
		return &genai.Schema{Type: genai.TypeArray, Items: items}, nil
	case reflect.Struct:
		return structSchema(t, in_progress)
	default:
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedType, t)
	}
}

func structSchema(t reflect.Type, in_progress map[reflect.Type]bool) (*genai.Schema, error) {
	if in_progress[t] {
		return nil, fmt.Errorf("%w: %v is recursive", ErrUnsupportedType, t)
	}
	in_progress[t] = true
	defer delete(in_progress, t)

	schema := &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{},
	}
	for i := range t.NumField() {
		field := t.Field(i)
		// checked first since encoding/json uses the fields of
		// embedded structs even when their type is unexported
		if field.Anonymous {
			return nil, fmt.Errorf("%w: embedded field %s in %v", ErrUnsupportedType, field.Name, t)
		}
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}

		field_schema, err := schemaForType(field.Type, in_progress)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		field_schema.Description = field.Tag.Get("description")
		if enum := field.Tag.Get("enum"); enum != "" {
			if field_schema.Type != genai.TypeString {
				return nil, fmt.Errorf("%w: enum tag on non string field %s", ErrUnsupportedType, field.Name)
			}
			field_schema.Enum = strings.Split(enum, ",")
		}

		schema.Properties[name] = field_schema
		schema.PropertyOrdering = append(schema.PropertyOrdering, name)
		if !strings.Contains(options, "omitempty") {
			schema.Required = append(schema.Required, name)
		}
	}
	return schema, nil
}
