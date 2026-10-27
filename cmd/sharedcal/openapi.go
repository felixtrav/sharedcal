package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/url"

	"github.com/felixtrav/sharedcal/internal/api"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/realtime"
)

// printOpenAPI writes the generated OpenAPI document without needing a
// database or identity provider (used to generate the TypeScript client).
func printOpenAPI(w io.Writer) error {
	log := slog.New(slog.DiscardHandler)
	u, _ := url.Parse("http://localhost:8080")
	a, err := auth.New(auth.Config{AppURL: u, Secret: make([]byte, 32)}, &auth.Sessions{}, nil, log)
	if err != nil {
		return err
	}
	broker := realtime.NewLocal(1)
	srv := api.New(api.Deps{Domain: &domain.Service{}, Auth: a, Broker: broker, Presence: realtime.NewPresence(broker), Log: log, Version: version})
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(srv.OpenAPI())
}
