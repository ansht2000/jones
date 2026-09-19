package llm

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/genai"
)

type schemaInner struct {
	Name string `json:"name"`
}

type schemaTest struct {
	Text     string       `json:"text" description:"some text"`
	Choice   string       `json:"choice" enum:"a,b"`
	Count    int          `json:"count"`
	Score    float64      `json:"score"`
	Ok       bool         `json:"ok"`
	Tags     []string     `json:"tags,omitempty"`
	Inner    schemaInner  `json:"inner"`
	Optional *schemaInner `json:"optional,omitempty"`
	Data     []byte       `json:"data"`
	NoTag    string
	Skipped  string `json:"-"`
	Dash     string `json:"-,"`
	private  string
}

func TestSchemaFor(t *testing.T) {
	inner := func() *genai.Schema {
		return &genai.Schema{
			Type:             genai.TypeObject,
			Properties:       map[string]*genai.Schema{"name": {Type: genai.TypeString}},
			PropertyOrdering: []string{"name"},
			Required:         []string{"name"},
		}
	}
	optional := inner()
	optional.Nullable = genai.Ptr(true)

	expected := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"text":     {Type: genai.TypeString, Description: "some text"},
			"choice":   {Type: genai.TypeString, Enum: []string{"a", "b"}},
			"count":    {Type: genai.TypeInteger},
			"score":    {Type: genai.TypeNumber},
			"ok":       {Type: genai.TypeBoolean},
			"tags":     {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			"inner":    inner(),
			"optional": optional,
			"data":     {Type: genai.TypeString},
			"NoTag":    {Type: genai.TypeString},
			"-":        {Type: genai.TypeString},
		},
		PropertyOrdering: []string{"text", "choice", "count", "score", "ok", "tags", "inner", "optional", "data", "NoTag", "-"},
		Required:         []string{"text", "choice", "count", "score", "ok", "inner", "data", "NoTag", "-"},
	}

	schema, err := SchemaFor(&schemaTest{})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if !reflect.DeepEqual(schema, expected) {
		got, _ := json.MarshalIndent(schema, "", "  ")
		want, _ := json.MarshalIndent(expected, "", "  ")
		t.Errorf("Expected schema:\n%s\ngot:\n%s\n", want, got)
	}
}

func TestSchemaForNilPointer(t *testing.T) {
	// only the type matters, so a nil pointer works
	schema, err := SchemaFor((*schemaInner)(nil))
	if err != nil || schema.Type != genai.TypeObject {
		t.Errorf("Expected an object schema, got %v, %v\n", schema, err)
	}
}

func TestSchemaForSlice(t *testing.T) {
	schema, err := SchemaFor(&[]schemaInner{})
	if err != nil || schema.Type != genai.TypeArray || schema.Items.Type != genai.TypeObject {
		t.Errorf("Expected an array of objects, got %v, %v\n", schema, err)
	}
}

type recursiveNode struct {
	Children []recursiveNode `json:"children"`
}

type embeddedStruct struct {
	schemaInner
}

func TestSchemaForUnsupported(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"not a pointer", schemaInner{}},
		{"nil", nil},
		{"map", &map[string]string{}},
		{"interface field", &struct {
			Value any `json:"value"`
		}{}},
		{"map field", &struct {
			Values map[string]int `json:"values"`
		}{}},
		{"recursive", &recursiveNode{}},
		{"embedded", &embeddedStruct{}},
		{"enum on non string", &struct {
			Count int `json:"count" enum:"1,2"`
		}{}},
	}
	for _, c := range cases {
		if _, err := SchemaFor(c.v); !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("%s: expected error %v, got %v\n", c.name, ErrUnsupportedType, err)
		}
	}
}
