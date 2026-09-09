package ci

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type robotTokenMock struct {
	mu          sync.Mutex
	robots      map[int]map[string]any
	vault       map[string]map[string]any
	nextID      int
	creates     int
	updates     int
	refreshes   int
	vaultWrites int
}

func newRobotTokenMock() *robotTokenMock {
	return &robotTokenMock{
		robots: make(map[int]map[string]any),
		vault:  make(map[string]map[string]any),
		nextID: 42,
	}
}

func (m *robotTokenMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/secret/data/"):
		m.readVault(w, strings.TrimPrefix(path, "/v1/secret/data/"))
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/secret/data/"):
		m.writeVault(w, r, strings.TrimPrefix(path, "/v1/secret/data/"))
	case r.Method == http.MethodGet && path == "/api/v2.0/robots":
		m.listRobots(w)
	case r.Method == http.MethodPost && path == "/api/v2.0/robots":
		m.createRobot(w, r)
	case strings.HasPrefix(path, "/api/v2.0/robots/"):
		m.updateRobot(w, r, path)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": path})
	}
}

func (m *robotTokenMock) readVault(w http.ResponseWriter, path string) {
	data, ok := m.vault[path]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"data": data}})
}

func (m *robotTokenMock) writeVault(w http.ResponseWriter, r *http.Request, path string) {
	var body struct {
		Data map[string]any `json:"data"`
	}
	decodeJSON(r, &body)
	m.vault[path] = body.Data
	m.vaultWrites++
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"version": m.vaultWrites}})
}

func (m *robotTokenMock) listRobots(w http.ResponseWriter) {
	ids := make([]int, 0, len(m.robots))
	for id := range m.robots {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	robots := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		robots = append(robots, m.robots[id])
	}
	writeJSON(w, http.StatusOK, robots)
}

func (m *robotTokenMock) createRobot(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	decodeJSON(r, &body)
	id := m.nextID
	m.nextID++
	m.creates++
	robot := map[string]any{
		"id":          id,
		"name":        "robot$" + body["name"].(string),
		"description": body["description"],
		"level":       "system",
		"disable":     false,
		"expires_at":  -1,
		"permissions": body["permissions"],
	}
	m.robots[id] = robot
	response := cloneMap(robot)
	response["secret"] = fmt.Sprintf("CreatedSecret%d", id)
	writeJSON(w, http.StatusCreated, response)
}

func (m *robotTokenMock) updateRobot(w http.ResponseWriter, r *http.Request, path string) {
	id, err := strconv.Atoi(strings.TrimPrefix(path, "/api/v2.0/robots/"))
	if err != nil || m.robots[id] == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": path})
		return
	}
	var body map[string]any
	decodeJSON(r, &body)
	switch r.Method {
	case http.MethodPut:
		m.updates++
		for key, value := range body {
			m.robots[id][key] = value
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	case http.MethodPatch:
		m.refreshes++
		writeJSON(w, http.StatusOK, map[string]any{"secret": fmt.Sprintf("RefreshedSecret%d", id)})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{})
	}
}

func TestHarborRobotTokens(t *testing.T) {
	mock := newRobotTokenMock()
	server := httptest.NewServer(mock)
	defer server.Close()

	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Dir(filepath.Dir(source))
	playbook := filepath.Join(repoRoot, "ci", "playbooks", "harbor-robot-tokens-contracts.yml")
	cmd := exec.Command("ansible-playbook", playbook, "-e", "mock_base_url="+server.URL)
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ansible robot-token contract failed: %v\n%s", err, output)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.creates != 2 || mock.updates != 0 || mock.refreshes != 1 || mock.vaultWrites != 3 {
		t.Fatalf("unexpected reconciliation counts: creates=%d updates=%d refreshes=%d vault_writes=%d",
			mock.creates, mock.updates, mock.refreshes, mock.vaultWrites)
	}

	assertRobotPermissions(t, mock.robots[42], []string{"*"}, []string{"pull"})
	assertRobotPermissions(t, mock.robots[43], []string{"dockerhub", "quay"}, []string{"pull", "push"})
	assertVaultToken(t, mock.vault["shared/harbor/cluster-pull"], "robot$ci-cluster-pull", "ci-initial", "pull", []string{"*"})
	assertVaultToken(t, mock.vault["shared/harbor/builder"], "robot$ci-builder", "ci-second", "pull_push", []string{"dockerhub", "quay"})
	if got := mock.vault["shared/harbor/builder"]["password"]; got != "RefreshedSecret43" {
		t.Fatalf("rotated builder password = %v, want RefreshedSecret43", got)
	}
}

func assertRobotPermissions(t *testing.T, robot map[string]any, projects, actions []string) {
	t.Helper()
	permissions, ok := robot["permissions"].([]any)
	if !ok || len(permissions) != len(projects) {
		t.Fatalf("permissions = %#v, want %d entries", robot["permissions"], len(projects))
	}
	for index, project := range projects {
		permission := permissions[index].(map[string]any)
		if permission["kind"] != "project" || permission["namespace"] != project {
			t.Fatalf("permission %d = %#v, want project %q", index, permission, project)
		}
		access := permission["access"].([]any)
		gotActions := make([]string, 0, len(access))
		for _, entry := range access {
			gotActions = append(gotActions, entry.(map[string]any)["action"].(string))
		}
		if !reflect.DeepEqual(gotActions, actions) {
			t.Fatalf("actions for %q = %v, want %v", project, gotActions, actions)
		}
	}
}

func assertVaultToken(t *testing.T, token map[string]any, username, rotationID, mode string, projects []string) {
	t.Helper()
	if token == nil {
		t.Fatalf("OpenBao token for %q was not written", username)
	}
	if token["username"] != username || token["rotation_id"] != rotationID || token["mode"] != mode {
		t.Fatalf("OpenBao token metadata = %#v", token)
	}
	gotProjects := anyStrings(token["projects"])
	if !reflect.DeepEqual(gotProjects, projects) {
		t.Fatalf("OpenBao projects = %v, want %v", gotProjects, projects)
	}
	var dockerConfig struct {
		Auths map[string]struct {
			Username string `json:"username"`
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(token[".dockerconfigjson"].(string)), &dockerConfig); err != nil {
		t.Fatalf("decode .dockerconfigjson: %v", err)
	}
	if got := dockerConfig.Auths["harbor.example.com"].Username; got != username {
		t.Fatalf("docker config username = %q, want %q", got, username)
	}
}

func anyStrings(value any) []string {
	items := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.(string))
	}
	return result
}

func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func decodeJSON(r *http.Request, target any) {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		panic(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		panic(err)
	}
}
