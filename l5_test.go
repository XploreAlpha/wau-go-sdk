// Package wau — l5_test.go
//
// v1.0.0 M11 P4.5 L5 包管理器 client 测试(per D72/D73/D74,2026-07-10)。
//
// 模式:httptest + 真 mux,跟现有 agents_test / skills_test 一致。
// 覆盖:5 client method + 错误码 + 边界。
package wau

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockL5Server 模拟 wau-core-kernel /v1/l5/* 端点(对应 MemoryL5Store 行为)
func mockL5Server(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	store := newMockL5Store()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/l5/install", func(w http.ResponseWriter, r *http.Request) {
		var req L5InstallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := store.Install(req)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/l5/uninstall", func(w http.ResponseWriter, r *http.Request) {
		var req L5UninstallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := store.Uninstall(req)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/l5/update", func(w http.ResponseWriter, r *http.Request) {
		var req L5UpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, _ := store.Update(req)
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/l5/search", func(w http.ResponseWriter, r *http.Request) {
		var req L5SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res := store.Search(req)
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /v1/l5/login", func(w http.ResponseWriter, r *http.Request) {
		var req L5LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := store.Login(req)
		if err != nil {
			http.Error(w, err.Error(), 401)
			return
		}
		writeJSON(w, res)
	})
	srv := httptest.NewServer(mux)
	return srv, srv.Close
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// mockL5Store 跟 MemoryL5Store 等价(简化版,只用于 test mock)
type mockL5Store struct {
	installed map[string]map[string]bool
	registry  []L5SearchHit
	users     map[string]string
}

func newMockL5Store() *mockL5Store {
	return &mockL5Store{
		installed: map[string]map[string]bool{
			"alice": {},
			"bob":   {},
		},
		registry: []L5SearchHit{
			{Name: "weather-agent", Version: "1.2.3", Description: "Get current weather", Universe: "general", TrustScore: 0.95},
			{Name: "reminder-agent", Version: "0.9.1", Description: "Set reminders", Universe: "productivity", TrustScore: 0.88},
		},
		users: map[string]string{"alice": "alice-pwd", "bob": "bob-pwd"},
	}
}

func (m *mockL5Store) Install(req L5InstallRequest) (*L5InstallResponse, error) {
	if req.UserID == "" || req.AgentName == "" {
		return nil, errMock("user_id and agent_name required")
	}
	// 查 registry
	for _, h := range m.registry {
		if h.Name == req.AgentName {
			if m.installed[req.UserID][req.AgentName] && !req.Purge {
				return nil, errMock("agent already installed")
			}
			m.installed[req.UserID][req.AgentName] = true
			return &L5InstallResponse{
				OK:              true,
				AgentID:         "agent-" + req.UserID + "-" + req.AgentName,
				Version:         h.Version,
				InstalledAt:     1720598400,
				DurationMS:      23.5,
				SandboxDockerID: "docker-" + req.AgentName,
			}, nil
		}
	}
	return nil, errMock("agent not found in registry")
}

func (m *mockL5Store) Uninstall(req L5UninstallRequest) (*L5UninstallResponse, error) {
	if req.UserID == "" || req.AgentName == "" {
		return nil, errMock("user_id and agent_name required")
	}
	if !m.installed[req.UserID][req.AgentName] {
		return nil, errMock("agent not installed")
	}
	if req.Purge {
		delete(m.installed[req.UserID], req.AgentName)
		return &L5UninstallResponse{OK: true, UninstalledAt: 1720598500}, nil
	}
	return &L5UninstallResponse{OK: true, UninstalledAt: 1720598500, SnapshotPath: "/var/lib/snapshots/x.tar.gz"}, nil
}

func (m *mockL5Store) Update(req L5UpdateRequest) (*L5UpdateResponse, error) {
	if req.UserID == "" {
		return nil, errMock("user_id required")
	}
	names := []string{}
	if req.AgentName != "" {
		names = []string{req.AgentName}
	} else {
		for n := range m.installed[req.UserID] {
			names = append(names, n)
		}
	}
	return &L5UpdateResponse{OK: true, UpdatedCount: len(names), UpdatedAgents: names}, nil
}

func (m *mockL5Store) Search(req L5SearchRequest) *L5SearchResponse {
	hits := []L5SearchHit{}
	for _, h := range m.registry {
		if req.Query != "" && !contains(h.Name, req.Query) && !contains(h.Description, req.Query) {
			continue
		}
		if req.Universe != "" && h.Universe != req.Universe {
			continue
		}
		hits = append(hits, h)
	}
	return &L5SearchResponse{OK: true, Results: hits, Total: len(hits)}
}

func (m *mockL5Store) Login(req L5LoginRequest) (*L5LoginResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, errMock("username and password required")
	}
	if pwd, ok := m.users[req.Username]; !ok || pwd != req.Password {
		return nil, errMock("invalid credentials")
	}
	return &L5LoginResponse{
		OK: true, AccessToken: "access-" + req.Username, RefreshToken: "refresh-" + req.Username,
		ExpiresAt: 1720602000, UserID: req.Username,
	}, nil
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type errMock string

func (e errMock) Error() string { return string(e) }

// TestL5_Install_Success
func TestL5_Install_Success(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.L5().Install(context.Background(), L5InstallRequest{
		UserID: "alice", AgentName: "weather-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.AgentID == "" || resp.Version != "1.2.3" {
		t.Errorf("got %+v", resp)
	}
}

// TestL5_Install_NotFound
func TestL5_Install_NotFound(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	_, err := c.L5().Install(context.Background(), L5InstallRequest{
		UserID: "alice", AgentName: "no-such-agent",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestL5_Install_AlreadyInstalled
func TestL5_Install_AlreadyInstalled(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	req := L5InstallRequest{UserID: "alice", AgentName: "weather-agent"}
	if _, err := c.L5().Install(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	_, err := c.L5().Install(context.Background(), req)
	if err == nil {
		t.Fatal("expected conflict")
	}
}

// TestL5_Uninstall_Default_KeepsSnapshot
func TestL5_Uninstall_Default_KeepsSnapshot(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	ctx := context.Background()
	if _, err := c.L5().Install(ctx, L5InstallRequest{UserID: "alice", AgentName: "weather-agent"}); err != nil {
		t.Fatal(err)
	}
	resp, err := c.L5().Uninstall(ctx, L5UninstallRequest{
		UserID: "alice", AgentName: "weather-agent", Purge: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.SnapshotPath == "" {
		t.Errorf("got %+v", resp)
	}
}

// TestL5_Uninstall_Purge
func TestL5_Uninstall_Purge(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	ctx := context.Background()
	if _, err := c.L5().Install(ctx, L5InstallRequest{UserID: "alice", AgentName: "weather-agent"}); err != nil {
		t.Fatal(err)
	}
	resp, err := c.L5().Uninstall(ctx, L5UninstallRequest{
		UserID: "alice", AgentName: "weather-agent", Purge: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.SnapshotPath != "" {
		t.Errorf("got %+v, want empty SnapshotPath on purge", resp)
	}
}

// TestL5_Update_All
func TestL5_Update_All(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	ctx := context.Background()
	if _, err := c.L5().Install(ctx, L5InstallRequest{UserID: "alice", AgentName: "weather-agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.L5().Install(ctx, L5InstallRequest{UserID: "alice", AgentName: "reminder-agent"}); err != nil {
		t.Fatal(err)
	}
	resp, err := c.L5().Update(ctx, L5UpdateRequest{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.UpdatedCount != 2 {
		t.Errorf("got %+v, want UpdatedCount=2", resp)
	}
}

// TestL5_Search
func TestL5_Search(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	resp, err := c.L5().Search(context.Background(), L5SearchRequest{
		UserID: "alice", Query: "weather", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Total < 1 || resp.Results[0].Name != "weather-agent" {
		t.Errorf("got %+v", resp)
	}
}

// TestL5_Login_Success
func TestL5_Login_Success(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	resp, err := c.L5().Login(context.Background(), L5LoginRequest{
		Username: "alice", Password: "alice-pwd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.UserID != "alice" || resp.AccessToken == "" {
		t.Errorf("got %+v", resp)
	}
}

// TestL5_Login_BadPassword
func TestL5_Login_BadPassword(t *testing.T) {
	srv, cleanup := mockL5Server(t)
	defer cleanup()
	c, _ := New(srv.URL)
	defer c.Close()
	_, err := c.L5().Login(context.Background(), L5LoginRequest{
		Username: "alice", Password: "wrong",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}