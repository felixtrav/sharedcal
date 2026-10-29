package domain

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/felixtrav/sharedcal/internal/store"
)

func TestNormalizeSelect(t *testing.T) {
	single := FieldDef{Key: "r", Label: "R", Kind: FieldSelect, Options: []string{"A", "B", "C"}}
	multi := single
	yes := true
	multi.Multiple = &yes

	for name, c := range map[string]struct {
		def  FieldDef
		in   any
		want any
		bad  bool
	}{
		"single string":              {single, "B", "B", false},
		"single from one-element":    {single, []any{"B"}, "B", false},
		"single refuses two":         {single, []any{"A", "B"}, nil, true},
		"single unknown":             {single, "Z", nil, true},
		"multi list in option order": {multi, []any{"C", "A", "C"}, []string{"A", "C"}, false},
		"multi accepts lone string":  {multi, "B", []string{"B"}, false},
		"multi unknown":              {multi, []any{"A", "Z"}, nil, true},
		"multi empty stores nothing": {multi, []any{}, nil, false},
		"wrong type":                 {multi, 3.0, nil, true},
	} {
		got, msg := normalizeSelect(c.def, c.in)
		if (msg != "") != c.bad || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v %q, want %#v bad=%v", name, got, msg, c.want, c.bad)
		}
	}
}

func TestValidateFieldDefsMultipleOnlyForSelect(t *testing.T) {
	yes := true
	out, fe := validateFieldDefs([]FieldDef{{Key: "a", Label: "A", Kind: FieldText, Multiple: &yes}})
	if len(fe) != 0 || out[0].Multiple != nil {
		t.Errorf("multiple on a text field should be dropped, got %+v %v", out[0], fe)
	}
}

func TestNormalizeMembersSingle(t *testing.T) {
	no := false
	one := FieldDef{Key: "g", Label: "G", Kind: FieldMembers, Multiple: &no}
	if _, msg := normalizeValue(one, []any{"a", "b"}); msg == "" {
		t.Error("two people in a single-person field should be refused")
	}
	if v, msg := normalizeValue(one, []any{"a"}); msg != "" || !reflect.DeepEqual(v, []string{"a"}) {
		t.Errorf("one person is fine, got %v %q", v, msg)
	}
	// Fields saved before the flag existed keep taking several people.
	legacy := FieldDef{Key: "g", Label: "G", Kind: FieldMembers}
	if _, msg := normalizeValue(legacy, []any{"a", "b"}); msg != "" {
		t.Errorf("unset flag on members means several, got %q", msg)
	}
}

func TestTitleTemplateSections(t *testing.T) {
	defs := []FieldDef{
		{Key: "name", Label: "Name", Kind: FieldText, Required: true},
		{Key: "at", Label: "At", Kind: FieldTime},
		{Key: "note", Label: "Note", Kind: FieldLongText},
	}
	typ := store.EventType{Name: "Release", TitleTemplate: "{{name}} Releases{{#at}} @{{at}}{{/at}}"}
	b, _ := json.Marshal(defs)
	typ.FieldsJSON = string(b)

	for name, c := range map[string]struct {
		vals map[string]any
		want string
	}{
		"name only":     {map[string]any{"name": "Game X"}, "Game X Releases"},
		"name and time": {map[string]any{"name": "Game X", "at": "19:30"}, "Game X Releases @19:30"},
		"blank time":    {map[string]any{"name": "Game X", "at": "  "}, "Game X Releases"},
	} {
		if got := renderTitle(typ, c.vals, nil, nil); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}

	for name, c := range map[string]struct {
		tpl string
		ok  bool
	}{
		"valid":                 {"{{name}}{{#at}} @{{at}}{{/at}}", true},
		"nested":                {"{{name}}{{#at}} @{{at}}{{#name}}!{{/name}}{{/at}}", true},
		"unclosed":              {"{{name}}{{#at}} @{{at}}", false},
		"closed wrong":          {"{{name}}{{#at}} x{{/name}}", false},
		"only optional":         {"{{#at}}{{at}}{{/at}}", false},
		"unknown section field": {"{{name}}{{#when}} x{{/when}}", false},
		"section on long text":  {"{{name}}{{#note}} x{{/note}}", false},
		"placeholder on a time": {"{{name}} {{at}}", true},
	} {
		_, err := checkTitleTemplate(c.tpl, defs)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", name, err, c.ok)
		}
	}
}

func TestNormalizeTime(t *testing.T) {
	d := FieldDef{Key: "at", Label: "At", Kind: FieldTime}
	for in, ok := range map[any]bool{"19:30": true, " 07:05 ": true, "24:00": false, "7:30": false, "19:30:00": false, "": false, 1930.0: false} {
		_, msg := normalizeValue(d, in)
		if (msg == "") != ok {
			t.Errorf("%#v: %q", in, msg)
		}
	}
}
