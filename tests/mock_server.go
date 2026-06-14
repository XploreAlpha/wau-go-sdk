// Package tests provides a mock WAU-core-kernel server for SDK contract tests.
//
// mock kernel 加载 5 场景黄金 JSON(在 ./contract-golden/),根据 prompt 路由响应
// 用途:
//   - wau-go-sdk 内部 SDK 单测
//   - 跨 SDK 一致性契约测试(黄金 JSON 唯一真相源)
//   - 本地开发 / CI 不依赖真 kernel
//
// 用法:
//
//	func TestXxx(t *testing.T) {
//	    srv := tests.NewMockKernel(t)
//	    defer srv.Close()
//	    c := wau.New(srv.URL)
//	    resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{Prompt: "What is the capital of France?"})
//	    ...
//	}
package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// GoldenScenario 一个 5 场景黄金 JSON 的内存表示
type GoldenScenario struct {
	Scenario                string            `json:"scenario"`
	Prompt                  string            `json:"prompt"`
	ExpectedSelectedAgent   string            `json:"expected_selected_agent"`
	ExpectedStatus          string            `json:"expected_status"`
	ExpectedResponseTokens  map[string][]string `json:"expected_response_tokens"`
	KernelResponse          map[string]any    `json:"kernel_response"`
}

// NewMockKernel 启动一个 mock WAU-core-kernel,根据 5 场景黄金 JSON 响应
func NewMockKernel(t *testing.T) *httptest.Server {
	t.Helper()

	goldenDir := findGoldenDir(t)
	scenarios := loadGoldenScenarios(t, goldenDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// 健康检查
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "ok",
				"version": "v0.6.0-mock",
				"uptime":  1.0,
				"redis":   "connected",
			})
			return

		// Kernel 元信息
		case r.Method == http.MethodGet && r.URL.Path == "/kernel/info":
			writeJSON(w, http.StatusOK, map[string]any{
				"version":     "v0.6.0-mock",
				"startTime":   "2026-06-14T00:00:00Z",
				"uptime":      60,
				"agentsCount": 3,
				"tasksCount":  5,
			})
			return

		// Agent 列表
		case r.Method == http.MethodGet && r.URL.Path == "/registry/agents":
			writeJSON(w, http.StatusOK, map[string]any{
				"agents": []map[string]any{
					{"name": "Whis", "url": "http://whis:18800", "skills": []string{"general", "analyze-data"}, "status": "online", "trust": 0.85},
					{"name": "Jarvis", "url": "http://jarvis:18800", "skills": []string{"clinical-decision-support", "clinical-diagnostic-reasoning"}, "status": "online", "trust": 0.92},
					{"name": "Benny", "url": "http://benny:18800", "skills": []string{"pharmaceutical"}, "status": "online", "trust": 0.88},
				},
				"total":      3,
				"page":       1,
				"pageSize":   10,
				"totalPages": 1,
			})
			return

		// Agent 状态
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/registry/agents/") && strings.HasSuffix(r.URL.Path, "/status"):
			name := strings.TrimPrefix(r.URL.Path, "/registry/agents/")
			name = strings.TrimSuffix(name, "/status")
			writeJSON(w, http.StatusOK, map[string]any{
				"name":   name,
				"status": "online",
				"trust":  0.90,
				"load":   map[string]any{"activeTasks": 1, "maxCapacity": 10, "cpuUsage": 0.2, "memoryUsage": 0.3},
				"circuit": "closed",
			})
			return

		// Agent 评分
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/registry/agents/") && strings.HasSuffix(r.URL.Path, "/score"):
			name := strings.TrimPrefix(r.URL.Path, "/registry/agents/")
			name = strings.TrimSuffix(name, "/score")
			writeJSON(w, http.StatusOK, map[string]any{
				"name":        name,
				"totalScore":  0.88,
				"trustScore":  0.90,
				"skillMatch":  0.85,
				"healthScore": 0.95,
				"loadScore":   0.80,
			})
			return

		// Agent 注册
		case r.Method == http.MethodPost && r.URL.Path == "/registry/agents/register":
			writeJSON(w, http.StatusCreated, map[string]any{"name": "test-agent", "registered": true})
			return

		// Agent 注销
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/registry/agents/"):
			writeJSON(w, http.StatusOK, map[string]any{"name": "test-agent", "deregistered": true})
			return

		// Agent 心跳
		case r.Method == http.MethodPost && r.URL.Path == "/registry/agents/heartbeat":
			writeJSON(w, http.StatusOK, map[string]any{"received": true})
			return

		// 负载上报
		case r.Method == http.MethodPost && r.URL.Path == "/heartbeat/load":
			writeJSON(w, http.StatusOK, map[string]any{"received": true})
			return

		// L4 真发 A2A(主测试场景)
		case r.Method == http.MethodPost && r.URL.Path == "/registry/tasks/submit":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
			prompt, _ := req["prompt"].(string)
			// 模拟 kernel 端 binding:"required" 校验
			if prompt == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":   "prompt is required (binding:required)",
					"field":   "prompt",
					"message": "Key: 'SubmitRequest.Prompt' Error:Field validation for 'Prompt' failed on the 'required' tag",
				})
				return
			}
			scenario := matchScenario(scenarios, prompt)
			if scenario == nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"task_id":        "task-unknown",
					"status":         "completed",
					"selected_agent": "Whis",
					"score":          0.50,
					"response":       "Echo: " + prompt,
				})
				return
			}
			writeJSON(w, http.StatusOK, scenario.KernelResponse)
			return

		// L3 决策(不真发)
		case r.Method == http.MethodPost && r.URL.Path == "/registry/tasks/simulate":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
				return
			}
			prompt, _ := req["prompt"].(string)
			scenario := matchScenario(scenarios, prompt)
			if scenario == nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"selected_agent": "Whis",
					"score":          0.50,
					"candidates":     []any{},
				})
				return
			}
			// 只返 decision 字段
			decision, _ := scenario.KernelResponse["decision"].(map[string]any)
			writeJSON(w, http.StatusOK, decision)
			return

		// 任务查询
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/registry/tasks/"):
			taskID := strings.TrimPrefix(r.URL.Path, "/registry/tasks/")
			writeJSON(w, http.StatusOK, map[string]any{
				"taskId":        taskID,
				"message":       "echo",
				"sourcePeer":    "mock",
				"status":        "completed",
				"assignedAgent": "Whis",
				"createdAt":     1718342400,
				"updatedAt":     1718342401,
			})
			return

		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "mock: route not found: " + r.Method + " " + r.URL.Path})
		}
	}))

	t.Cleanup(srv.Close)
	return srv
}

// ============================
// helpers
// ============================

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func matchScenario(scenarios []GoldenScenario, prompt string) *GoldenScenario {
	for i := range scenarios {
		if scenarios[i].Prompt == prompt {
			return &scenarios[i]
		}
	}
	return nil
}

func findGoldenDir(t *testing.T) string {
	t.Helper()
	// tests/ → contract-golden/
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// 兼容 cwd != tests/:向上找
	for i := 0; i < 5; i++ {
		try := filepath.Join(dir, "tests", "contract-golden")
		if _, err := os.Stat(try); err == nil {
			return try
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("contract-golden dir not found")
	return ""
}

var (
	loadOnce  sync.Once
	loadCache []GoldenScenario
	loadErr   error
)

func loadGoldenScenarios(t *testing.T, dir string) []GoldenScenario {
	t.Helper()
	loadOnce.Do(func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			loadErr = err
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				loadErr = err
				return
			}
			var s GoldenScenario
			if err := json.Unmarshal(data, &s); err != nil {
				loadErr = err
				return
			}
			loadCache = append(loadCache, s)
		}
	})
	if loadErr != nil {
		t.Fatalf("load golden: %v", loadErr)
	}
	return loadCache
}
