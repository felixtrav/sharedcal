# Atlas project for generating SharedCal migrations from the GORM models.
# Run from the repository root, one environment per dialect:
#
#   atlas migrate diff add_something --config file://deploy/atlas/atlas.hcl --env sqlite
#   atlas migrate diff add_something --config file://deploy/atlas/atlas.hcl --env postgres
#   atlas migrate diff add_something --config file://deploy/atlas/atlas.hcl --env mysql
#
# The desired schema comes from tools/atlasloader (its own Go module, so the
# app never depends on Atlas). The GORM models carry no foreign keys or CHECK
# constraints (those live only in the SQL migrations), so ALWAYS review the
# generated files and delete any DROP CONSTRAINT / foreign-key / check removals
# Atlas proposes. See CONTRIBUTING.md -> "Adding a migration".

data "external_schema" "sqlite" {
  program = ["go", "run", "-C", "tools/atlasloader", ".", "sqlite"]
}

data "external_schema" "postgres" {
  program = ["go", "run", "-C", "tools/atlasloader", ".", "postgres"]
}

data "external_schema" "mysql" {
  program = ["go", "run", "-C", "tools/atlasloader", ".", "mysql"]
}

env "sqlite" {
  src = data.external_schema.sqlite.url
  dev = "sqlite://dev?mode=memory"
  migration {
    dir    = "file://migrations/sqlite"
    format = golang-migrate
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}

env "postgres" {
  src = data.external_schema.postgres.url
  dev = "docker://postgres/16/dev?search_path=public"
  migration {
    dir    = "file://migrations/postgres"
    format = golang-migrate
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}

env "mysql" {
  src = data.external_schema.mysql.url
  dev = "docker://mysql/8.4/dev"
  migration {
    dir    = "file://migrations/mysql"
    format = golang-migrate
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}
