package domain

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
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

func TestImportThemeFiles(t *testing.T) {
	dir := t.TempDir()
	s := &Service{DB: storetest.Open(t), Broker: realtime.NewLocal(8), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Opts: Options{DataDir: dir, BuiltinThemes: os.DirFS("../../web/src/themes")}}
	ctx := context.Background()
	admin := authz.Actor{ID: "a", Role: authz.Admin, Active: true}
	write := func(name string, th Theme) {
		t.Helper()
		b, _ := json.Marshal(th)
		if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "themes", name), b, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() map[string]Theme {
		t.Helper()
		all, err := s.ListThemes(ctx, admin, true)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]Theme{}
		for _, th := range all {
			out[th.ID] = th
		}
		return out
	}
	activity := func() int64 {
		var n int64
		s.DB.R.Model(&store.Activity{}).Where("action = ?", "theme.installed").Count(&n)
		return n
	}

	mono := validTheme()
	write("mono.json", mono)
	bad := validTheme()
	bad.ID, bad.Modes["light"]["primary"] = "bad", "red;}"
	write("bad.json", bad)
	clash := validTheme()
	clash.ID = "default"
	write("default.json", clash)
	if err := s.ImportThemeFiles(ctx); err != nil {
		t.Fatal(err)
	}
	got := stored()
	if th, ok := got["mono"]; !ok || th.Builtin || th.Name != "Mono" {
		t.Fatalf("mono not imported: %+v", got)
	}
	if _, ok := got["bad"]; ok {
		t.Error("invalid theme file imported")
	}
	if got["default"].Name == "Mono" {
		t.Error("a file must not replace a built-in theme")
	}

	// The themes now come from the database: the directory is not read again.
	if err := os.RemoveAll(filepath.Join(dir, "themes")); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored()["mono"]; !ok {
		t.Fatal("imported theme vanished with its file")
	}

	// Restarting with an unchanged file writes nothing; an edited file updates the stored copy.
	write("mono.json", mono)
	before := activity()
	if err := s.ImportThemeFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if activity() != before {
		t.Error("unchanged file was imported again")
	}
	mono.Name = "Mono 2"
	write("mono.json", mono)
	if err := s.ImportThemeFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if stored()["mono"].Name != "Mono 2" || activity() != before+1 {
		t.Error("edited file did not update the stored theme")
	}
}
