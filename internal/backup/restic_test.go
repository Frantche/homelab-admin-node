package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResticConfigMultiRepo(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte(`RESTIC_REPOSITORIES="local sftp"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_REPOSITORY_LOCAL="/srv/restic"
RESTIC_PASSWORD_LOCAL="local-pass"
RESTIC_FORGET_ARGS_LOCAL="none"
RESTIC_REPOSITORY_SFTP="sftp:backup:/srv/restic"
RESTIC_PASSWORD_SFTP="sftp-pass"
AWS_ACCESS_KEY_ID_SFTP="ignored-but-parsed"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 2 || cfg.Repositories[0] != "local" || !cfg.InitRepositories {
		t.Fatalf("config = %#v", cfg)
	}
	if got := cfg.RepoValues["LOCAL"]["RESTIC_PASSWORD"]; got != "local-pass" {
		t.Fatalf("LOCAL password = %q", got)
	}
	if got := cfg.RepoValues["SFTP"]["AWS_ACCESS_KEY_ID"]; got != "ignored-but-parsed" {
		t.Fatalf("SFTP env = %q", got)
	}
}

func TestLoadResticConfigSingleQuotedValues(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "backup.env")
	if err := os.WriteFile(envFile, []byte(`RESTIC_DEFAULT_FORGET_ARGS='--keep-last 3 --prune'
RESTIC_REPOSITORIES=local
RESTIC_REPOSITORY_LOCAL=/srv/admin/backups/restic
RESTIC_PASSWORD_LOCAL='ci-restic-pass'
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadResticConfig(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultForgetArgs != "--keep-last 3 --prune" {
		t.Fatalf("forget args = %q", cfg.DefaultForgetArgs)
	}
	if got := cfg.RepoValues["LOCAL"]["RESTIC_PASSWORD"]; got != "ci-restic-pass" {
		t.Fatalf("password = %q", got)
	}
}

func TestValidateSecureRepository(t *testing.T) {
	if err := validateSecureRepository("ftp://example/restic", true); err == nil {
		t.Fatal("expected insecure ftp repository to fail")
	}
	if err := validateSecureRepository("sftp:backup:/srv/restic", true); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureRepository("ftp://example/restic", false); err != nil {
		t.Fatal(err)
	}
}
