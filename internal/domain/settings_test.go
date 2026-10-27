package domain

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

func TestUpdateSettingsValidation(t *testing.T) {
	s := &Service{DB: storetest.Open(t), Broker: realtime.NewLocal(8), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Opts: Options{Roles: auth.RoleMapping{DefaultRole: authz.Viewer}, AttachmentMaxSize: 1000, AttachmentMaxPerEvent: 10, AttachmentMaxPerComment: 5}}
	admin := authz.Actor{ID: "a", Role: authz.Admin, Active: true}
	ctx := context.Background()
	base, err := s.GetSettings(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Settings){
		"empty name":     func(st *Settings) { st.CalendarName = " " },
		"bad tz":         func(st *Settings) { st.DefaultTimeZone = "Nowhere" },
		"bad role":       func(st *Settings) { st.DefaultRole = "owner" },
		"css accent":     func(st *Settings) { st.AccentColor = "red;}" },
		"above ceiling":  func(st *Settings) { st.AttachmentMaxSize = 1001 },
		"negative quota": func(st *Settings) { st.AttachmentQuota = -1 },
		"too many/event": func(st *Settings) { st.AttachmentMaxPerEvent = 5000 },
	}
	for name, m := range bad {
		st := *base
		m(&st)
		if _, err := s.UpdateSettings(ctx, admin, st); ErrorKind(err) != KindInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	st := *base
	st.CalendarName, st.EnabledThemes = "Club", []string{"unknown"}
	got, err := s.UpdateSettings(ctx, admin, st)
	if err != nil || got.CalendarName != "Club" || len(got.EnabledThemes) != 1 || got.EnabledThemes[0] != "default" {
		t.Fatalf("update: %+v %v", got, err)
	}
	if _, err := s.UpdateSettings(ctx, authz.Actor{ID: "w", Role: authz.Writer, Active: true}, st); ErrorKind(err) != KindForbidden {
		t.Errorf("writer update: %v", err)
	}
}
