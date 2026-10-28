package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Theme is a token set compiled to CSS custom properties by the client.
type Theme struct {
	ID      string                       `json:"id" maxLength:"64"`
	Name    string                       `json:"name" maxLength:"64"`
	Author  string                       `json:"author" maxLength:"64"`
	Builtin bool                         `json:"builtin" readOnly:"true"`
	Modes   map[string]map[string]string `json:"modes"`
}

const themeKeyPrefix = "theme:"

var (
	themeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	themeHex       = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	themeLength    = regexp.MustCompile(`^(?:0|[0-9]{1,2}(?:\.[0-9]{1,3})?(?:rem|px|em))$`)
	themeFonts     = map[string]bool{"Inter": true, "Geist": true, "system-ui": true}
	requiredTokens = []string{"background", "foreground", "primary", "muted", "border"}
	colorTokens    = map[string]bool{
		"background": true, "foreground": true, "primary": true, "primary-foreground": true, "muted": true,
		"muted-foreground": true, "border": true, "card": true, "card-foreground": true, "popover": true,
		"popover-foreground": true, "secondary": true, "secondary-foreground": true, "accent": true,
		"accent-foreground": true, "destructive": true, "destructive-foreground": true, "ring": true, "input": true,
		"tag-1": true, "tag-2": true, "tag-3": true, "tag-4": true, "tag-5": true, "tag-6": true, "tag-7": true, "tag-8": true,
	}
	// Text/surface pairs that must reach WCAG AA (4.5:1) when both are set.
	contrastPairs = [][2]string{
		{"foreground", "background"}, {"muted-foreground", "background"}, {"muted-foreground", "muted"},
		{"primary-foreground", "primary"}, {"card-foreground", "card"}, {"popover-foreground", "popover"},
		{"secondary-foreground", "secondary"}, {"accent-foreground", "accent"}, {"destructive-foreground", "destructive"},
	}
)

func validThemeID(id string) bool { return themeIDPattern.MatchString(id) }

// ValidateTheme enforces the theme schema: allow-listed keys only, values
// that are hex colors, short lengths or allow-listed font names (so a theme
// can never inject CSS), and AA contrast for text/surface pairs.
func ValidateTheme(t *Theme) error {
	var fe fieldErrors
	if !validThemeID(t.ID) {
		fe.add("id", "id must be lowercase letters, digits and dashes")
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || utf8.RuneCountInString(t.Name) > 64 || strings.ContainsAny(t.Name, "<>") {
		fe.add("name", "name must be 1–64 characters")
	}
	if utf8.RuneCountInString(t.Author) > 64 || strings.ContainsAny(t.Author, "<>") {
		fe.add("author", "author is limited to 64 characters")
	}
	if len(t.Modes) != 2 || t.Modes["light"] == nil || t.Modes["dark"] == nil {
		fe.add("modes", "a theme defines exactly the light and dark modes")
	}
	for _, mode := range []string{"light", "dark"} {
		tokens := t.Modes[mode]
		if tokens == nil {
			continue
		}
		for _, k := range requiredTokens {
			if tokens[k] == "" {
				fe.add("modes."+mode+"."+k, "%s is required", k)
			}
		}
		if len(tokens) > 64 {
			fe.add("modes."+mode, "too many tokens")
		}
		for k, v := range tokens {
			path := "modes." + mode + "." + k
			switch {
			case colorTokens[k]:
				if !themeHex.MatchString(v) {
					fe.add(path, "must be a #rgb or #rrggbb color")
				}
			case k == "radius":
				if !themeLength.MatchString(v) {
					fe.add(path, "must be a length like 0.5rem")
				}
			case k == "font":
				if !themeFonts[v] {
					fe.add(path, "font must be Inter, Geist or system-ui")
				}
			default:
				fe.add(path, "unknown token %q", k)
			}
		}
		if len(fe) > 0 {
			continue
		}
		for _, pair := range contrastPairs {
			fg, bg := tokens[pair[0]], tokens[pair[1]]
			if fg == "" || bg == "" {
				continue
			}
			if r := contrastRatio(fg, bg); r < 4.5 {
				fe.add("modes."+mode+"."+pair[0], "contrast of %s on %s is %.2f:1; WCAG AA needs 4.5:1", pair[0], pair[1], r)
			}
		}
	}
	return fe.err()
}

func parseHex(h string) (r, g, b float64) {
	h = strings.TrimPrefix(h, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	return float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255
}

func luminance(h string) float64 {
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r, g, b := parseHex(h)
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func contrastRatio(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// builtinThemes reads the shipped themes. Invalid files are a build bug.
func (s *Service) builtinThemes() ([]Theme, error) {
	if s.Opts.BuiltinThemes == nil {
		return nil, nil
	}
	files, err := fs.Glob(s.Opts.BuiltinThemes, "*.json")
	if err != nil {
		return nil, err
	}
	var out []Theme
	for _, f := range files {
		b, err := fs.ReadFile(s.Opts.BuiltinThemes, f)
		if err != nil {
			return nil, err
		}
		var t Theme
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("builtin theme %s: %w", f, err)
		}
		t.Builtin = true
		out = append(out, t)
	}
	return out, nil
}

// ImportThemeFiles copies the drop-in themes in DATA_DIR/themes/*.json into
// the database at startup, so nothing reads the directory at runtime (and an
// instance on PostgreSQL or MySQL with S3 storage needs no data volume). A
// file is written when its theme is missing or differs from the stored copy;
// invalid files and files that reuse a built-in id are skipped with a warning.
func (s *Service) ImportThemeFiles(ctx context.Context) error {
	if s.Opts.DataDir == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(s.Opts.DataDir, "themes", "*.json"))
	if len(files) == 0 {
		return nil
	}
	builtin, err := s.builtinThemes()
	if err != nil {
		return err
	}
	reserved := map[string]bool{}
	for _, b := range builtin {
		reserved[b.ID] = true
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || len(b) > 64<<10 {
			s.Log.Warn("skipping theme file", "file", f, "err", err, "bytes", len(b))
			continue
		}
		var t Theme
		if err := json.Unmarshal(b, &t); err != nil {
			s.Log.Warn("skipping theme file", "file", f, "err", err)
			continue
		}
		if err := ValidateTheme(&t); err != nil {
			s.Log.Warn("skipping invalid theme file", "file", f, "err", err)
			continue
		}
		if reserved[t.ID] {
			s.Log.Warn("skipping theme file that reuses a built-in id", "file", f, "id", t.ID)
			continue
		}
		var changed bool
		err = s.write(ctx, authz.Actor{}, func(tx *Tx) error {
			changed, err = tx.saveTheme(t, true)
			return err
		})
		if err != nil {
			return fmt.Errorf("import theme %s: %w", f, err)
		}
		if changed {
			s.Log.Info("imported theme file", "file", f, "id", t.ID)
		}
	}
	return nil
}

// saveTheme stores t as an uploaded theme, records it and announces the
// change. With onlyIfChanged, an identical stored copy is left alone.
func (tx *Tx) saveTheme(t Theme, onlyIfChanged bool) (bool, error) {
	t.Builtin = false
	data, _ := json.Marshal(t)
	row := store.Setting{Key: themeKeyPrefix + t.ID, ValueJSON: string(data), CreatedAt: tx.now, UpdatedAt: tx.now}
	var existing store.Setting
	if err := byKey(tx.db, row.Key).Take(&existing).Error; err == nil {
		if onlyIfChanged && existing.ValueJSON == row.ValueJSON {
			return false, nil
		}
		row.CreatedAt = existing.CreatedAt
		if err := tx.db.Save(&row).Error; err != nil {
			return false, err
		}
	} else if isNotFound(err) {
		if err := tx.db.Create(&row).Error; err != nil {
			return false, err
		}
	} else {
		return false, err
	}
	if err := tx.record("theme.installed", "theme", t.ID, nil, nil, map[string]string{"id": t.ID, "name": t.Name}); err != nil {
		return false, err
	}
	return true, tx.emit("settings.changed", settingsKey, 0, struct{}{}, "")
}

func (s *Service) uploadedThemes(db *gorm.DB) ([]Theme, error) {
	var rows []store.Setting
	if err := db.Where(clause.Like{Column: clause.Column{Name: "key"}, Value: themeKeyPrefix + "%"}).Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []Theme
	for _, r := range rows {
		var t Theme
		if json.Unmarshal([]byte(r.ValueJSON), &t) == nil {
			t.Builtin = false
			out = append(out, t)
		}
	}
	return out, nil
}

// allThemes merges built-in and stored themes; built-in ones win on ID
// collisions.
func (s *Service) allThemes(db *gorm.DB) ([]Theme, error) {
	builtin, err := s.builtinThemes()
	if err != nil {
		return nil, err
	}
	uploaded, err := s.uploadedThemes(db)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []Theme{}
	for _, list := range [][]Theme{builtin, uploaded} {
		for _, t := range list {
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].ID == "default") != (out[j].ID == "default") {
			return out[i].ID == "default"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *Service) themeIDs(db *gorm.DB) (map[string]bool, error) {
	all, err := s.allThemes(db)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, t := range all {
		ids[t.ID] = true
	}
	return ids, nil
}

// ListThemes returns the enabled themes (or every installed theme for
// admins asking for all).
func (s *Service) ListThemes(ctx context.Context, actor authz.Actor, all bool) ([]Theme, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	themes, err := s.allThemes(db)
	if err != nil {
		return nil, err
	}
	if all && authz.CanAdminister(actor) {
		return themes, nil
	}
	st, err := s.loadSettings(db)
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{"default": true}
	for _, id := range st.EnabledThemes {
		enabled[id] = true
	}
	out := themes[:0:0]
	for _, t := range themes {
		if enabled[t.ID] {
			out = append(out, t)
		}
	}
	return out, nil
}

// InstallTheme validates and stores an uploaded theme.
func (s *Service) InstallTheme(ctx context.Context, actor authz.Actor, t Theme) (*Theme, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	if err := ValidateTheme(&t); err != nil {
		return nil, err
	}
	t.Builtin = false
	err := s.write(ctx, actor, func(tx *Tx) error {
		builtin, err := s.builtinThemes()
		if err != nil {
			return err
		}
		for _, b := range builtin {
			if b.ID == t.ID {
				return invalidField("id", "a built-in theme already uses this id")
			}
		}
		_, err = tx.saveTheme(t, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// RemoveTheme deletes a stored theme. Built-in themes cannot be removed. A
// theme imported from DATA_DIR/themes comes back at the next start unless its
// file is deleted too.
func (s *Service) RemoveTheme(ctx context.Context, actor authz.Actor, id string) error {
	if !authz.CanAdminister(actor) {
		return forbidden("admins only")
	}
	if !validThemeID(id) {
		return notFound("theme")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		res := byKey(tx.db, themeKeyPrefix+id).Delete(&store.Setting{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return notFound("uploaded theme")
		}
		if err := tx.record("theme.removed", "theme", id, nil, nil, nil); err != nil {
			return err
		}
		return tx.emit("settings.changed", settingsKey, 0, struct{}{}, "")
	})
}
