package ci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type adguardRewrite struct {
	Domain string `json:"domain"`
	Answer string `json:"answer"`
}

type adguardHomeMock struct {
	mu          sync.Mutex
	rewrites    []adguardRewrite
	adds        int
	updates     int
	rejectAuth  bool
	authChecked int
}

func (m *adguardHomeMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	username, password, ok := r.BasicAuth()
	m.authChecked++
	if !ok || username != "ci-user" || password != "ci-password" || m.rejectAuth {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/control/rewrite/list":
		writeJSON(w, http.StatusOK, m.rewrites)
	case r.Method == http.MethodPost && r.URL.Path == "/control/rewrite/add":
		var rewrite adguardRewrite
		if err := json.NewDecoder(r.Body).Decode(&rewrite); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m.rewrites = append(m.rewrites, rewrite)
		m.adds++
		writeJSON(w, http.StatusOK, map[string]any{})
	case r.Method == http.MethodPut && r.URL.Path == "/control/rewrite/update":
		var request struct {
			Target adguardRewrite `json:"target"`
			Update adguardRewrite `json:"update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for index := range m.rewrites {
			if m.rewrites[index] == request.Target {
				m.rewrites[index] = request.Update
				m.updates++
				writeJSON(w, http.StatusOK, map[string]any{})
				return
			}
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func TestAdGuardHomeDNSContracts(t *testing.T) {
	mock := &adguardHomeMock{rewrites: []adguardRewrite{{Domain: "existing.example.test", Answer: "192.0.2.19"}}}
	server := httptest.NewServer(mock)
	defer server.Close()

	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Dir(filepath.Dir(source))
	playbook := filepath.Join(repoRoot, "ci", "playbooks", "adguardhome-contracts.yml")
	run := func() (string, error) {
		cmd := exec.Command("ansible-playbook", "-i", "localhost,", "-c", "local", playbook, "-e", "mock_base_url="+server.URL)
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		return string(output), err
	}

	if output, err := run(); err != nil {
		t.Fatalf("first AdGuard Home reconciliation failed: %v\n%s", err, output)
	}
	if output, err := run(); err != nil {
		t.Fatalf("second AdGuard Home reconciliation failed: %v\n%s", err, output)
	}

	mock.mu.Lock()
	if mock.adds != 1 || mock.updates != 1 || len(mock.rewrites) != 2 || mock.authChecked != 4 {
		t.Fatalf("unexpected reconciliation: adds=%d updates=%d rewrites=%d auth_checks=%d", mock.adds, mock.updates, len(mock.rewrites), mock.authChecked)
	}
	mock.rejectAuth = true
	mock.mu.Unlock()

	output, err := run()
	if err == nil {
		t.Fatalf("invalid AdGuard Home credentials unexpectedly succeeded:\n%s", output)
	}
	if strings.Contains(output, "ci-password") {
		t.Fatalf("Ansible output exposed the AdGuard Home password:\n%s", output)
	}
}
