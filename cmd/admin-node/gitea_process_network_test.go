package main

import (
	"reflect"
	"testing"
)

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
