package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validTheme() Theme {
	return Theme{ID: "mono", Name: "Mono", Author: "me", Modes: map[string]map[string]string{
		"light": {"background": "#ffffff", "foreground": "#111111", "primary": "#000000", "muted": "#eeeeee", "border": "#dddddd", "radius": "0.5rem", "font": "Inter", "tag-1": "#aa0000"},
		"dark":  {"background": "#000", "foreground": "#eee", "primary": "#fff", "muted": "#222", "border": "#333"},
	}}
}

func TestValidateTheme(t *testing.T) {
	ok := validTheme()
	if err := ValidateTheme(&ok); err != nil {
		t.Fatalf("valid theme refused: %v", err)
	}
	mutate := map[string]func(*Theme){
		"css injection":   func(th *Theme) { th.Modes["light"]["primary"] = "red;}body{background:url(x)" },
		"url()":           func(th *Theme) { th.Modes["light"]["background"] = "url(javascript:alert(1))" },
		"named color":     func(th *Theme) { th.Modes["light"]["border"] = "red" },
		"unknown token":   func(th *Theme) { th.Modes["light"]["--evil"] = "#000000" },
		"bad radius":      func(th *Theme) { th.Modes["light"]["radius"] = "calc(1px+env(x))" },
		"font not listed": func(th *Theme) { th.Modes["light"]["font"] = "Comic Sans\";}" },
		"low contrast":    func(th *Theme) { th.Modes["dark"]["foreground"] = "#111111" },
		"missing token":   func(th *Theme) { delete(th.Modes["dark"], "border") },
		"extra mode":      func(th *Theme) { th.Modes["sepia"] = map[string]string{} },
		"bad id":          func(th *Theme) { th.ID = "../x" },
		"markup in name":  func(th *Theme) { th.Name = "<script>" },
	}
	for name, m := range mutate {
		th := validTheme()
		m(&th)
		if err := ValidateTheme(&th); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBuiltinThemesAreValid(t *testing.T) {
	files, _ := filepath.Glob("../../web/src/themes/*.json")
	if len(files) == 0 {
		t.Skip("web themes not present")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var th Theme
		if err := json.Unmarshal(b, &th); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if err := ValidateTheme(&th); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestContrastRatio(t *testing.T) {
	if r := contrastRatio("#000000", "#ffffff"); r < 20.9 || r > 21.1 {
		t.Errorf("black/white: %v", r)
	}
	if r := contrastRatio("#777", "#777777"); r != 1 {
		t.Errorf("same: %v", r)
	}
}
