package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// env returns a Getenv over the minimal valid configuration plus overrides.
// An override of "" unsets the key.
func env(overrides map[string]string) Getenv {
	m := map[string]string{
		"APP_URL":            "https://cal.example.com",
		"OIDC_ISSUER":        "https://id.example.com",
		"OIDC_CLIENT_ID":     "sharedcal",
		"OIDC_CLIENT_SECRET": "secret",
	}
	for k, v := range overrides {
		m[k] = v
	}
	return func(k string) string { return m[k] }
}

func mustLoad(t *testing.T, overrides map[string]string) *Config {
	t.Helper()
	c, err := LoadFrom(env(overrides))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	return c
}

func TestDefaults(t *testing.T) {
	c := mustLoad(t, nil)
	if c.AppURL.String() != "https://cal.example.com" || !c.Secure() {
		t.Errorf("app url: %v secure=%v", c.AppURL, c.Secure())
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"ListenAddr", c.ListenAddr, ":8080"},
		{"DataDir", c.DataDir, "/data"},
		{"DBDriver", c.DBDriver, "sqlite"},
		{"DBDSN", c.DBDSN, "/data/sharedcal.db"},
		{"OIDCScopes", strings.Join(c.OIDCScopes, " "), "openid profile email groups"},
		{"OIDCRoleClaim", c.OIDCRoleClaim, "groups"},
		{"DefaultRole", c.DefaultRole, RoleViewer},
		{"AllowUnmapped", c.AllowUnmapped, true},
		{"SessionIdleTTL", c.SessionIdleTTL, 168 * time.Hour},
		{"SessionMaxTTL", c.SessionMaxTTL, 720 * time.Hour},
		{"LogLevel", c.LogLevel, "info"},
		{"LogFormat", c.LogFormat, "json"},
		{"MetricsAddr", c.MetricsAddr, ""},
		{"AttachmentMaxSize", c.AttachmentMaxSize, int64(25 << 20)},
		{"AttachmentMaxPerEvent", c.AttachmentMaxPerEvent, 10},
		{"AttachmentMaxPerComment", c.AttachmentMaxPerComment, 5},
		{"AttachmentQuota", c.AttachmentQuota, int64(2 << 30)},
		{"BackupKeep", c.BackupKeep, 7},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}
	if c.SessionSecret != nil {
		t.Error("session secret should be left for LoadOrCreateSessionSecret")
	}
}

func TestOverrides(t *testing.T) {
	c := mustLoad(t, map[string]string{
		"APP_URL":              "http://localhost:8080/",
		"DATA_DIR":             "/srv/cal",
		"OIDC_ADMIN_GROUPS":    " calendar-admins , ops ,,",
		"OIDC_WRITER_GROUPS":   "calendar-writers",
		"DEFAULT_ROLE":         "Writer",
		"ALLOW_UNMAPPED_USERS": "false",
		"TRUSTED_PROXIES":      "10.0.0.0/8, 192.168.1.5, ::1",
		"LOG_LEVEL":            "DEBUG",
		"LOG_FORMAT":           "text",
		"METRICS_ADDR":         "127.0.0.1:9090",
		"ATTACHMENT_MAX_SIZE":  "10MB",
		"ATTACHMENT_QUOTA":     "1GiB",
		"BACKUP_KEEP":          "0",
	})
	if c.AppURL.String() != "http://localhost:8080" || c.Secure() {
		t.Errorf("trailing slash should be trimmed and http is not secure: %v", c.AppURL)
	}
	if c.DBDSN != filepath.Join("/srv/cal", "sharedcal.db") {
		t.Errorf("sqlite DSN should follow DATA_DIR: %s", c.DBDSN)
	}
	if strings.Join(c.OIDCAdminGroups, "|") != "calendar-admins|ops" {
		t.Errorf("admin groups: %q", c.OIDCAdminGroups)
	}
	if c.DefaultRole != RoleWriter || c.AllowUnmapped {
		t.Errorf("role mapping: %s %v", c.DefaultRole, c.AllowUnmapped)
	}
	if len(c.TrustedProxies) != 3 || c.TrustedProxies[1].String() != "192.168.1.5/32" || c.TrustedProxies[2].String() != "::1/128" {
		t.Errorf("trusted proxies: %v", c.TrustedProxies)
	}
	if c.LogLevel != "debug" || c.LogFormat != "text" {
		t.Errorf("logging: %s %s", c.LogLevel, c.LogFormat)
	}
	if c.AttachmentMaxSize != 10<<20 || c.AttachmentQuota != 1<<30 || c.BackupKeep != 0 {
		t.Errorf("limits: %d %d %d", c.AttachmentMaxSize, c.AttachmentQuota, c.BackupKeep)
	}
}

// Every problem is reported at once, so operators fix them in one pass.
func TestErrorsAreCollected(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{
		"APP_URL":            "",
		"OIDC_ISSUER":        "",
		"OIDC_CLIENT_SECRET": "",
		"LOG_LEVEL":          "loud",
	}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"APP_URL", "OIDC_ISSUER", "OIDC_CLIENT_SECRET", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestInvalidValues(t *testing.T) {
	cases := []struct {
		key, value, want string
	}{
		{"APP_URL", "cal.example.com", "absolute http(s) URL"},
		{"APP_URL", "ftp://cal.example.com", "absolute http(s) URL"},
		{"DB_DRIVER", "oracle", "DB_DRIVER"},
		{"OIDC_SCOPES", "profile email", "must include openid"},
		{"DEFAULT_ROLE", "owner", "DEFAULT_ROLE"},
		{"ALLOW_UNMAPPED_USERS", "maybe", "true or false"},
		{"SESSION_SECRET", "too-short", "at least 32"},
		{"SESSION_IDLE_TTL", "1000h", "must not exceed"},
		{"SESSION_MAX_TTL", "-1h", "positive duration"},
		{"SESSION_IDLE_TTL", "a week", "positive duration"},
		{"TRUSTED_PROXIES", "10.0.0.0/33", "invalid CIDR"},
		{"TRUSTED_PROXIES", "proxy.local", "invalid CIDR"},
		{"CSP_IMG_SOURCES", "http://cdn.example.com", "https origin"},
		{"CSP_IMG_SOURCES", "https://cdn.example.com/a", "https origin"},
		{"CSP_IMG_SOURCES", "https://x.com;script-src *", "https origin"},
		{"LOG_FORMAT", "xml", "LOG_FORMAT"},
		{"METRICS_ADDR", "9090", "host:port"},
		{"ATTACHMENT_MAX_SIZE", "lots", "invalid size"},
		{"ATTACHMENT_QUOTA", "0", "invalid size"},
		{"ATTACHMENT_MAX_PER_EVENT", "-1", "non-negative integer"},
		{"BACKUP_KEEP", "seven", "non-negative integer"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			_, err := LoadFrom(env(map[string]string{tc.key: tc.value}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestExternalDatabaseNeedsDSN(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		if _, err := LoadFrom(env(map[string]string{"DB_DRIVER": driver})); err == nil || !strings.Contains(err.Error(), "DB_DSN is required") {
			t.Errorf("%s without DSN: %v", driver, err)
		}
		c := mustLoad(t, map[string]string{"DB_DRIVER": strings.ToUpper(driver), "DB_DSN": "dsn://x"})
		if c.DBDriver != driver || c.DBDSN != "dsn://x" {
			t.Errorf("%s: %s %s", driver, c.DBDriver, c.DBDSN)
		}
	}
}

func TestBlobStore(t *testing.T) {
	if c := mustLoad(t, nil); c.BlobStore != "fs" {
		t.Errorf("default blob store %q", c.BlobStore)
	}
	s3 := map[string]string{
		"BLOB_STORE": "S3", "S3_ENDPOINT": "http://minio:9000/", "S3_BUCKET": "cal", "S3_PREFIX": "/prod/blobs/",
		"S3_ACCESS_KEY_ID": "id", "S3_SECRET_ACCESS_KEY": "key", "S3_FORCE_PATH_STYLE": "true",
	}
	c := mustLoad(t, s3)
	if c.BlobStore != "s3" || c.S3.Endpoint.String() != "http://minio:9000" || c.S3.Bucket != "cal" ||
		c.S3.Prefix != "prod/blobs" || c.S3.AccessKeyID != "id" || c.S3.SecretAccessKey != "key" || !c.S3.ForcePathStyle {
		t.Errorf("s3: %+v", c.S3)
	}
	// No static keys means the AWS credential chain (env, file, IAM role).
	c = mustLoad(t, map[string]string{"BLOB_STORE": "s3", "S3_ENDPOINT": "https://s3.amazonaws.com", "S3_BUCKET": "cal"})
	if c.S3.AccessKeyID != "" || c.S3.Region != "" {
		t.Errorf("chain credentials: %+v", c.S3)
	}

	cases := []struct {
		overrides map[string]string
		want      string
	}{
		{map[string]string{"BLOB_STORE": "gcs"}, "BLOB_STORE must be fs or s3"},
		{map[string]string{"BLOB_STORE": "s3"}, "S3_ENDPOINT is required"},
		{map[string]string{"BLOB_STORE": "s3", "S3_ENDPOINT": "https://s3.amazonaws.com"}, "S3_BUCKET is required"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "s3.amazonaws.com"}, "S3_ENDPOINT must be"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://s3.amazonaws.com/bucket"}, "S3_ENDPOINT must be"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://u:p@s3.amazonaws.com"}, "S3_ENDPOINT must be"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://s3.amazonaws.com", "S3_PREFIX": "a/../b"}, "S3_PREFIX"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://s3.amazonaws.com", "S3_PREFIX": "a//b"}, "S3_PREFIX"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://s3.amazonaws.com", "S3_ACCESS_KEY_ID": "id"}, "set both"},
		{map[string]string{"BLOB_STORE": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "https://s3.amazonaws.com", "S3_FORCE_PATH_STYLE": "yes please"}, "true or false"},
		{map[string]string{"S3_BUCKET": "b"}, "S3_BUCKET is set but BLOB_STORE is not s3"},
	}
	for _, tc := range cases {
		if _, err := LoadFrom(env(tc.overrides)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v, want error containing %q", tc.overrides, err, tc.want)
		}
	}
}

func TestSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	secret := strings.Repeat("s", 40)
	c := mustLoad(t, map[string]string{
		"OIDC_CLIENT_SECRET":      "",
		"OIDC_CLIENT_SECRET_FILE": write("client", "from-file\n"),
		"SESSION_SECRET_FILE":     write("session", "  "+secret+"\n"),
		"DB_DRIVER":               "postgres",
		"DB_DSN_FILE":             write("dsn", "postgres://u:p@db/cal\n"),
	})
	if c.OIDCClientSecret != "from-file" {
		t.Errorf("client secret: %q", c.OIDCClientSecret)
	}
	if string(c.SessionSecret) != secret {
		t.Errorf("session secret was not trimmed: %q", c.SessionSecret)
	}
	if c.DBDSN != "postgres://u:p@db/cal" {
		t.Errorf("dsn: %q", c.DBDSN)
	}

	// Both forms at once is ambiguous; a missing file is an error.
	if _, err := LoadFrom(env(map[string]string{"OIDC_CLIENT_SECRET_FILE": write("other", "x")})); err == nil || !strings.Contains(err.Error(), "only one of OIDC_CLIENT_SECRET") {
		t.Errorf("both set: %v", err)
	}
	if _, err := LoadFrom(env(map[string]string{"OIDC_CLIENT_SECRET": "", "OIDC_CLIENT_SECRET_FILE": filepath.Join(dir, "missing")})); err == nil || !strings.Contains(err.Error(), "OIDC_CLIENT_SECRET_FILE") {
		t.Errorf("missing file: %v", err)
	}
}

func TestParseSize(t *testing.T) {
	good := map[string]int64{
		"1":       1,
		"512B":    512,
		"512kb":   512 << 10,
		"25MB":    25 << 20,
		"25 MiB":  25 << 20,
		" 2GB ":   2 << 30,
		"3g":      3 << 30,
		"100K":    100 << 10,
		"1048576": 1 << 20,
	}
	for in, want := range good {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MB", "-5MB", "0", "1.5GB", "10TB", "99999999999999GB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) should fail", in)
		}
	}
}

func TestLoadOrCreateSessionSecret(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	c := &Config{DataDir: dir}
	if err := c.LoadOrCreateSessionSecret(); err != nil {
		t.Fatal(err)
	}
	if len(c.SessionSecret) != 64 {
		t.Fatalf("generated secret length %d", len(c.SessionSecret))
	}
	info, err := os.Stat(filepath.Join(dir, "session_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("secret file mode %v", info.Mode().Perm())
	}
	// A restart reuses the stored secret.
	again := &Config{DataDir: dir}
	if err := again.LoadOrCreateSessionSecret(); err != nil || string(again.SessionSecret) != string(c.SessionSecret) {
		t.Fatalf("reload: %v", err)
	}
	// An explicit secret wins and nothing is read.
	explicit := &Config{DataDir: filepath.Join(t.TempDir(), "unused"), SessionSecret: []byte("explicit")}
	if err := explicit.LoadOrCreateSessionSecret(); err != nil || string(explicit.SessionSecret) != "explicit" {
		t.Fatalf("explicit: %v", err)
	}
	if _, err := os.Stat(explicit.DataDir); !os.IsNotExist(err) {
		t.Error("explicit secret should not touch DATA_DIR")
	}
	// A truncated file is refused rather than silently weakening sessions.
	if err := os.WriteFile(filepath.Join(dir, "session_secret"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Config{DataDir: dir}).LoadOrCreateSessionSecret(); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("short secret file: %v", err)
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []string{RoleViewer, RoleWriter, RoleAdmin} {
		if !ValidRole(r) {
			t.Errorf("%s should be valid", r)
		}
	}
	for _, r := range []string{"", "Admin", "owner"} {
		if ValidRole(r) {
			t.Errorf("%q should be invalid", r)
		}
	}
}
