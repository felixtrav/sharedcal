// Command mockoidc runs the development-only OpenID Connect provider used by
// `make dev` and the Playwright tests. Never expose it to a network.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/felixtrav/sharedcal/internal/oidcmock"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("MOCKOIDC_ADDR", "127.0.0.1:9999")
	issuer := env("MOCKOIDC_ISSUER", "http://localhost:9999")
	h, err := oidcmock.New(issuer, env("MOCKOIDC_CLIENT_ID", "sharedcal"), env("MOCKOIDC_CLIENT_SECRET", "sharedcal-dev-secret"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("mockoidc: DEVELOPMENT ONLY identity provider at %s (listening on %s)", issuer, addr)
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
