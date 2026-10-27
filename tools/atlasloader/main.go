// Command atlasloader prints the DDL that GORM would create for SharedCal's
// models, for Atlas's external_schema data source (see deploy/atlas/atlas.hcl).
//
// It lives in its own module so the main module never depends on Atlas.
//
//	go run . sqlite|postgres|mysql
package main

import (
	"fmt"
	"io"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	"github.com/felixtrav/sharedcal/internal/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: atlasloader sqlite|postgres|mysql")
		os.Exit(2)
	}
	dialect := os.Args[1]
	switch dialect {
	case "sqlite", "postgres", "mysql":
	default:
		fmt.Fprintf(os.Stderr, "atlasloader: unsupported dialect %q\n", dialect)
		os.Exit(2)
	}
	stmts, err := gormschema.New(dialect).Load(store.AllModels()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlasloader: %v\n", err)
		os.Exit(1)
	}
	if _, err := io.WriteString(os.Stdout, stmts); err != nil {
		os.Exit(1)
	}
}
