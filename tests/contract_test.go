// Package tests — 5 场景契约测试
//
// 验证 wau-go-sdk 跟 mock kernel 的 5 场景契约:
//   - clinical  → Jarvis
//   - france    → Whis
//   - pain      → Benny
//   - sales     → Whis
//   - rare_disease → Jarvis
//
// 黄金 JSON 真相源在 ./contract-golden/scenario_*.json(ADR-0004 约定)
package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

// 5 场景 — 跟 wau-intent/e2e_test/test_submit_l4.py 对齐
var fiveScenarios = []struct {
	scene            string
	prompt           string
	expectedAgent    string
	mustHaveTokens   []string // 至少 1 个 token 出现在 response 文本里
	skipTokenCheck   bool     // 部分场景无中文 token
}{
	{"clinical", "I need clinical decision support for a patient", "Jarvis",
		[]string{"临床", "决策", "支持", "患者"}, false},
	{"france", "What is the capital of France?", "Whis",
		[]string{"paris"}, false},
	{"pain", "Recommend an over-the-counter pain reliever", "Benny",
		[]string{"ibuprofen", "acetaminophen", "pain", "reliever"}, false},
	{"sales", "Show me this quarter's sales analytics", "Whis",
		[]string{"sales", "analytics", "quarter"}, false},
	{"rare_disease", "Help me diagnose a rare disease", "Jarvis",
		[]string{"罕见病", "鉴别", "诊断"}, false},
}

func TestContract_5Scenarios_AllPass(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()

	c, err := wau.New(srv.URL, wau.WithRetryNo(), wau.WithTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, sc := range fiveScenarios {
		t.Run(sc.scene, func(t *testing.T) {
			resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{
				Prompt:    sc.prompt,
				TimeoutMs: 60000,
			})
			if err != nil {
				t.Fatalf("Submit(%q): %v", sc.prompt, err)
			}
			if resp.Status != "completed" {
				t.Errorf("status = %q, want completed", resp.Status)
			}
			if resp.SelectedAgent != sc.expectedAgent {
				t.Errorf("selected_agent = %q, want %q", resp.SelectedAgent, sc.expectedAgent)
			}
			if resp.Score <= 0 {
				t.Errorf("score = %f, want > 0", resp.Score)
			}
			// response 文本里至少出现 1 个期望 token
			if !sc.skipTokenCheck {
				rawResp, _ := json.Marshal(resp.Response)
				text := strings.ToLower(string(rawResp))
				matched := false
				for _, tok := range sc.mustHaveTokens {
					if strings.Contains(text, strings.ToLower(tok)) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("response 文本里没找到任何期望 token %v, got: %s", sc.mustHaveTokens, text)
				}
			}
		})
	}
}

// 验证 SDK SubmitRequest 字段以 kernel 真相源为准:Prompt 必填
// wau-cli 旧 DTO {Message, SourcePeer, ...} 调 L4 会失败(binding:"required" 拦截)
func TestContract_SubmitRequest_PromptRequired(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	c, _ := wau.New(srv.URL, wau.WithRetryNo())

	// 空 prompt → 期望返错(Prompt 必填)
	_, err := c.Tasks().Submit(context.Background(), wau.SubmitRequest{Prompt: ""})
	if err == nil {
		t.Error("空 prompt 应返错,但 err == nil")
	}
	// 错误应是 *APIError(400) 而非网络错
	if _, ok := err.(*wau.APIError); !ok {
		t.Logf("err type = %T (期望 APIError 400)", err)
	}
}

// 验证 agents/tasks/kernel 4 核心对象基础调用
func TestContract_KernelInfo_Health_Agents(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	c, _ := wau.New(srv.URL, wau.WithRetryNo())
	ctx := context.Background()

	// 1. Health
	h, err := c.Kernel().Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("health.status = %q, want ok", h.Status)
	}

	// 2. Info
	info, err := c.Kernel().Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version == "" {
		t.Error("info.version 缺失")
	}
	if info.AgentsCount != 3 {
		t.Errorf("info.agentsCount = %d, want 3", info.AgentsCount)
	}

	// 3. ListAgents
	agents, err := c.Agents().List(ctx, wau.PageOptions{PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents.Agents) != 3 {
		t.Errorf("agents count = %d, want 3", len(agents.Agents))
	}

	// 4. GetAgent(jarvis)
	jarvis, err := c.Agents().Get(ctx, "jarvis")
	if err != nil {
		t.Fatalf("Get jarvis: %v", err)
	}
	if jarvis.Status != "online" {
		t.Errorf("jarvis.status = %q, want online", jarvis.Status)
	}

	// 5. AgentScore
	score, err := c.Agents().Score(ctx, "jarvis")
	if err != nil {
		t.Fatalf("Score jarvis: %v", err)
	}
	if score.TotalScore <= 0 {
		t.Errorf("jarvis.totalScore = %f, want > 0", score.TotalScore)
	}
}

// 黄金 JSON 加载跟 mock kernel 用的是同一份(单一真相源)
func TestContract_GoldenJSON_SingleSource(t *testing.T) {
	dir := findGoldenDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	scenarioCount := 0
	handshakeCount := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		// v0.8.0 M5-1 B.1:握手黄金 JSON 独立统计(handshake_*.json 5 个)
		// v0.9.0 M3 §3.7:chat 黄金 JSON 独立统计(chat_*.json,数量不固定,跳过严格校验)
		if strings.HasPrefix(e.Name(), "handshake_") {
			handshakeCount++
		} else if strings.HasPrefix(e.Name(), "chat_") {
			// chat 黄金 JSON 独立统计,数量不固定,只 log 不 assert
		} else {
			scenarioCount++
		}
	}
	if scenarioCount != 5 {
		t.Errorf("scenario 黄金 JSON 文件数 = %d, want 5 (clinical/france/pain/sales/rare_disease)", scenarioCount)
	}
	// handshake_*.json 5 个 B.1.5 约束(per plan §B.1.5 + D.关键复用)
	if handshakeCount != 5 {
		t.Errorf("handshake 黄金 JSON 文件数 = %d, want 5 (handshake_happy/reuse/agent_not_found/tenant_mismatch/invalid_request)", handshakeCount)
	}
}

// 验证:每个黄金 JSON 必含 prompt + expected_selected_agent + expected_status + kernel_response
func TestContract_GoldenJSON_Schema(t *testing.T) {
	dir := findGoldenDir(t)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			// v0.8.0 M5-1 B.1:handshake_*.json 是握手端点契约(不同 schema),
			// schema 校验由 tests/handshake_contract_test.go 单独做
			if strings.HasPrefix(e.Name(), "handshake_") {
				if _, ok := got["endpoint"]; !ok {
					t.Errorf("握手黄金 JSON %s 缺 endpoint 字段", e.Name())
				}
				return
			}
			// v0.9.0 M3 §3.7:chat_*.json 是 wau-edge OpenAI 兼容层契约(不同 schema),
			// schema 校验由 tests/chat_contract_test.go 单独做
			if strings.HasPrefix(e.Name(), "chat_") {
				if _, ok := got["endpoint"]; !ok {
					t.Errorf("chat 黄金 JSON %s 缺 endpoint 字段", e.Name())
				}
				return
			}
			for _, k := range []string{"scenario", "prompt", "expected_selected_agent", "expected_status", "kernel_response"} {
				if _, ok := got[k]; !ok {
					t.Errorf("黄金 JSON %s 缺字段 %q", e.Name(), k)
				}
			}
		})
	}
}
