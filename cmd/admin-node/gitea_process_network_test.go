package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Frantche/homelab-admin-node/internal/config"
	"github.com/Frantche/homelab-admin-node/internal/operation"
	"github.com/Frantche/homelab-admin-node/internal/runner"
)

type successfulGiteaRestoreRunner struct{}

func (successfulGiteaRestoreRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	if name == "docker" && len(args) > 0 && args[0] == "inspect" {
		return runner.Result{Stdout: "true healthy\n"}
	}
	return runner.Result{}
}

func TestGiteaProcessNetworkArgs(t *testing.T) {
	tests := []struct {
		name     string
		database string
		egress   string
		want     []string
	}{
		{
			name:     "separate database and egress networks",
			database: "gitea-db",
			egress:   "admin-edge",
			want:     []string{"--network", "gitea-db", "--network", "admin-edge"},
		},
		{
			name:     "shared network is attached once",
			database: "shared",
			egress:   "shared",
			want:     []string{"--network", "shared"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := giteaProcessNetworkArgs(test.database, test.egress)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("giteaProcessNetworkArgs() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGiteaProcessWritableMountArgs(t *testing.T) {
	t.Run("separate restore and history directories", func(t *testing.T) {
		got, err := giteaProcessWritableMountArgs("/tmp/restore", "/tmp/history/backup.log")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-v", "/tmp/restore:/tmp/restore", "-v", "/tmp/history:/tmp/history"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("giteaProcessWritableMountArgs() = %v, want %v", got, want)
		}
	})

	t.Run("shared directory is mounted once", func(t *testing.T) {
		got, err := giteaProcessWritableMountArgs("/tmp/shared", "/tmp/shared/backup.log")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-v", "/tmp/shared:/tmp/shared"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("giteaProcessWritableMountArgs() = %v, want %v", got, want)
		}
	})

	for _, test := range []struct {
		name          string
		restoreTmp    string
		backupFileLog string
	}{
		{name: "relative restore path", restoreTmp: "tmp/restore", backupFileLog: "/tmp/history/backup.log"},
		{name: "relative history path", restoreTmp: "/tmp/restore", backupFileLog: "tmp/history/backup.log"},
		{name: "restore root", restoreTmp: "/", backupFileLog: "/tmp/history/backup.log"},
		{name: "history root", restoreTmp: "/tmp/restore", backupFileLog: "/backup.log"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := giteaProcessWritableMountArgs(test.restoreTmp, test.backupFileLog); err == nil {
				t.Fatal("expected unsafe writable path to be rejected")
			}
		})
	}
}

func TestGiteaProcessRestoreRefusesContendedOperationLock(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "operation.lock")
	modePath := filepath.Join(root, "mode")
	if err := os.WriteFile(modePath, []byte("normal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := operation.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	var out, errOut bytes.Buffer
	a := app{
		out:    &out,
		errOut: &errOut,
		cfg: config.Config{
			OperationLock: lockPath,
			ModeFile:      modePath,
		},
	}
	err = a.runGiteaProcessRestore(context.Background(), giteaProcessRestoreOptions{})
	if err == nil || !strings.Contains(err.Error(), "operation lock") {
		t.Fatalf("error = %v, want operation lock refusal", err)
	}
	modeValue, readErr := os.ReadFile(modePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := strings.TrimSpace(string(modeValue)); got != "normal" {
		t.Fatalf("mode = %q, want normal", got)
	}
}

func TestBackupGiteaProcessDatabasePublishesNonEmptyDump(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerPath := filepath.Join(binDir, "docker")
	dockerScript := "#!/usr/bin/env bash\nset -euo pipefail\n[[ \"$*\" == *\"pg_dump --format=custom\"* ]]\nprintf 'custom-dump'\n"
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dumpPath := filepath.Join(root, "safety", "gitea.dump")
	if err := os.Mkdir(filepath.Dir(dumpPath), 0o700); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	a := app{errOut: &errOut}
	if err := a.backupGiteaProcessDatabase(context.Background(), dumpPath); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "custom-dump" {
		t.Fatalf("dump content = %q", content)
	}
	info, err := os.Stat(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("dump mode = %o, want 600", got)
	}
	if _, err := os.Stat(dumpPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary dump remains: %v", err)
	}
}

func TestBackupGiteaProcessDatabaseRejectsEmptyDump(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dumpPath := filepath.Join(root, "gitea.dump")
	a := app{errOut: &bytes.Buffer{}}
	err := a.backupGiteaProcessDatabase(context.Background(), dumpPath)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %v, want empty dump refusal", err)
	}
	for _, path := range []string{dumpPath, dumpPath + ".tmp"} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("unexpected dump artifact %s: %v", path, statErr)
		}
	}
}

func TestRunGiteaProcessRestoreLifecycle(t *testing.T) {
	for _, test := range []struct {
		name      string
		fail      bool
		wantMode  string
		wantStart bool
	}{
		{name: "success resumes timers", wantMode: "normal", wantStart: true},
		{name: "failure leaves explicit state and timers stopped", fail: true, wantMode: "restore_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			timerLog := filepath.Join(root, "systemctl.log")
			systemctlScript := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  is-active|is-enabled)
    exit 0
    ;;
  stop|start)
    printf '%s\n' "$*" >> "${GITEA_RESTORE_TIMER_LOG:?}"
    ;;
  *)
    exit 1
    ;;
esac
`
			if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctlScript), 0o755); err != nil {
				t.Fatal(err)
			}
			dockerScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "exec" && "$*" == *" pg_dump "* ]]; then
  printf 'pre-restore-custom-dump'
  exit 0
fi
if [[ "${1:-}" == "run" ]]; then
  if [[ "${GITEA_RESTORE_FAIL:-false}" == "true" ]]; then
    exit 42
  fi
  printf '%s\n' '-- restored SQL' > "${GITEA_RESTORE_TMP:?}/dump.postgres.sql"
  exit 0
fi
exit 0
`
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(dockerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			rsyncScript := `#!/usr/bin/env bash
set -euo pipefail
destination="${!#}"
install -d -m 0700 "$destination"
printf 'safety-copy\n' > "${destination%/}/.restore-test"
`
			if err := os.WriteFile(filepath.Join(binDir, "rsync"), []byte(rsyncScript), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "chown"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GITEA_RESTORE_TIMER_LOG", timerLog)
			t.Setenv("GITEA_RESTORE_FAIL", strconv.FormatBool(test.fail))

			giteaStack := filepath.Join(root, "gitea-stack")
			for _, path := range []string{
				"gitea/git/repositories",
				"gitea/gitea/avatars",
				"gitea/gitea/repo-avatars",
				"gitea/gitea/attachments",
				"gitea/gitea/packages",
				"gitea/gitea/repo-archive",
			} {
				if err := os.MkdirAll(filepath.Join(giteaStack, path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			restoreTmp := filepath.Join(root, "restore-tmp")
			if err := os.Mkdir(restoreTmp, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GITEA_RESTORE_TMP", restoreTmp)
			processEnv := filepath.Join(root, "gitea-process.env")
			envContent := "RESTORE_TMP_FOLDER=" + restoreTmp + "\nBACKUP_FILE_LOG=" + filepath.Join(root, "history/backup.log") + "\n"
			if err := os.WriteFile(processEnv, []byte(envContent), 0o600); err != nil {
				t.Fatal(err)
			}
			modePath := filepath.Join(root, "mode")
			if err := os.WriteFile(modePath, []byte("normal\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			var out, errOut bytes.Buffer
			a := app{
				out:    &out,
				errOut: &errOut,
				cfg: config.Config{
					ModeFile:       modePath,
					OperationLock:  filepath.Join(root, "operation.lock"),
					GiteaStackPath: giteaStack,
				},
				runner: successfulGiteaRestoreRunner{},
			}
			preRestoreDir := filepath.Join(root, "pre-restore")
			err := a.runGiteaProcessRestore(context.Background(), giteaProcessRestoreOptions{
				BackupFilename: "gitea-backup-test.zip",
				ProcessEnv:     processEnv,
				GiteaEnv:       filepath.Join(root, "gitea.env"),
				GiteaCompose:   filepath.Join(root, "compose.yaml"),
				PreRestoreDir:  preRestoreDir,
				RunConverge:    false,
			})
			if test.fail && err == nil {
				t.Fatal("expected injected restore failure")
			}
			if !test.fail && err != nil {
				t.Fatalf("restore failed: %v; stderr=%s", err, errOut.String())
			}
			modeValue, readErr := os.ReadFile(modePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if got := strings.TrimSpace(string(modeValue)); got != test.wantMode {
				t.Fatalf("mode = %q, want %q", got, test.wantMode)
			}
			timerCalls, readErr := os.ReadFile(timerLog)
			if readErr != nil {
				t.Fatal(readErr)
			}
			hasStart := strings.Contains(string(timerCalls), "start admin-converge.timer admin-backup.timer admin-gitea-process-backup.timer")
			if hasStart != test.wantStart {
				t.Fatalf("timer calls = %q, want start=%t", timerCalls, test.wantStart)
			}
			if _, statErr := os.Stat(filepath.Join(preRestoreDir, "gitea.dump")); statErr != nil {
				t.Fatalf("pre-restore database dump missing: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(preRestoreDir, "gitea-data/.restore-test")); statErr != nil {
				t.Fatalf("pre-restore filesystem copy missing: %v", statErr)
			}
		})
	}
}
