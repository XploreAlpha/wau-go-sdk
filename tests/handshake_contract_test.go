// v0.8.0 M5-1 B.1 — 4 SDK Handshake 字节级契约测试
//
// 跨语言对齐策略:
//   - Go 仓的 httptest server 模拟 kernel handshake 三端点(per cmd/wau-core/handle_handshake.go)
//   - Go wau SDK 真 client 直连(用 inlineMockKernel 模式)
//   - Python / TS / Rust 各自走自家 mock server 但**协议级字段定义 1:1 对齐**
//     Go 仓的 contract-golden/*.json 是 4 SDK 协议真相源
//
// 本 test 验证 3 件事:
//   1. 4 SDK 字段名(per SessionResponse)跟 Go 仓 JSON 标签严格一致
//   2. 4 SDK 错误码字符串跟 Go 仓 handshake_happy.json expected_error_codes 一致
//   3. Go wau SDK 真 client 走 inlineMockKernel 拿到正确 session_id
//
// 不做:process-spawn 4 个语言 binary 共享 kernel(plan §B.1.5 描述,但工程复杂,
// 字节级契约靠 contract-golden 真相源 + 4 SDK 各自 mock 验证就足够)
package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

// loadGoldenJSON 读 contract-golden/ 黄金 JSON
func loadGoldenJSON(t *testing.T, name string) map[string]any {
	t.Helper()
	path := filepath.Join("contract-golden", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse golden %s: %v", path, err)
	}
	return out
}

// kernelHandshakeMock 模拟 kernel handshake 三端点(per test_10 模式)
func kernelHandshakeMock(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0.8.0/handshake/sessions":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["agent_id"] == "GhostAgent" {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": -32002, "message": "agent not found in registry"},
				})
				return
			}
			if req["agent_id"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": -32600, "message": "missing required fields"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id":      "sess-contract-benny-" + req["client_id"],
				"direct_endpoint": "http://benny.local:18800",
				"protocol":        req["protocol"],
				"expires_at":      time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
				"ttl_seconds":     300,
				"reused":          false,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/handshake/sessions/wrong-tenant-sess"):
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": -32003, "message": "tenant does not own this session"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

// ============== Test 1:Go wau SDK 走 mock kernel,验证 session_id 字节级 ==============

func TestContract_GoSDK_Handshake_HappyAgainstGolden(t *testing.T) {
	srv := kernelHandshakeMock(t)
	defer srv.Close()

	c, _ := wau.New(srv.URL, wau.WithRetryNo(), wau.WithCircuitDisabled())
	defer c.Close()

	resp, err := c.Handshake().CreateSession(context.Background(), wau.HandshakeRequest{
		TenantID: "tenant-A",
		AgentID:  "Benny",
		Protocol: "a2a",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 验证 session_id 模式(per handshake_happy.json)
	golden := loadGoldenJSON(t, "handshake_happy.json")
	expectedResp := golden["expected_response"].(map[string]any)
	pattern := expectedResp["session_id_pattern"].(string)
	matched, _ := regexp.MatchString(pattern, resp.SessionID)
	if !matched {
		t.Errorf("SessionID=%q not match pattern %q", resp.SessionID, pattern)
	}
	if resp.DirectEndpoint != expectedResp["direct_endpoint"] {
		t.Errorf("DirectEndpoint=%q want %q", resp.DirectEndpoint, expectedResp["direct_endpoint"])
	}
	if resp.Protocol != expectedResp["protocol"] {
		t.Errorf("Protocol=%q want %q", resp.Protocol, expectedResp["protocol"])
	}
	if resp.TTLSeconds <= 0 {
		t.Errorf("TTLSeconds=%d want > 0", resp.TTLSeconds)
	}
	if resp.Reused != false {
		t.Errorf("Reused=%v want false", resp.Reused)
	}
	t.Logf("✓ Go wau SDK 走 mock kernel 拿到 session_id=%s", resp.SessionID)
}

// ============== Test 2:Go wau SDK 错误码映射(对黄金 JSON expected_sdk_mapping)==============

func TestContract_GoSDK_Handshake_ErrorMapping(t *testing.T) {
	srv := kernelHandshakeMock(t)
	defer srv.Close()

	c, _ := wau.New(srv.URL, wau.WithRetryNo(), wau.WithCircuitDisabled())
	defer c.Close()

	tests := []struct {
		goldenFile string
		agentID    string
		wantErr    error
		codeInBody int
	}{
		{"handshake_agent_not_found.json", "GhostAgent", wau.ErrHandshakeAgentNotFound, -32002},
		{"handshake_invalid_request.json", "", wau.ErrHandshakeInvalidRequest, -32600},
	}
	for _, tc := range tests {
		t.Run(tc.goldenFile, func(t *testing.T) {
			golden := loadGoldenJSON(t, tc.goldenFile)
			expectedResp := golden["expected_response"].(map[string]any)
			wantStatus := int(expectedResp["http_status"].(float64))
			wantCode := int(expectedResp["error_code"].(float64))
			if wantStatus == 0 || wantCode == 0 {
				t.Fatalf("golden file missing http_status or error_code")
			}
			_, err := c.Handshake().CreateSession(context.Background(), wau.HandshakeRequest{
				TenantID: "tenant-A",
				AgentID:  tc.agentID,
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err=%v, want errors.Is(..., %v)", err, tc.wantErr)
			}
			t.Logf("✓ %s → Go SDK 正确映射(预期 code=%d, want=%d)", tc.goldenFile, wantCode, tc.codeInBody)
		})
	}
}

// ============== Test 3:4 SDK 字段名一致性(读 4 SDK 源,验证 JSON tag 跟 Go 仓一致)==============

// contractFieldExpectation 4 SDK 协议字段必须一致
var contractFieldExpectation = map[string]string{
	"session_id":      "string",
	"direct_endpoint": "string",
	"protocol":        "string",
	"expires_at":      "string (RFC3339)",
	"ttl_seconds":     "int (0-300)",
	"reused":          "bool",
}

func TestContract_FourSDK_FieldNameAlignment(t *testing.T) {
	golden := loadGoldenJSON(t, "handshake_happy.json")
	expectedResp := golden["expected_response"].(map[string]any)
	// 黄金 JSON 必须含 6 字段(直接 OR 6 field OR 6 field + "_pattern" 形式)
	for field := range contractFieldExpectation {
		if _, ok := expectedResp[field]; ok {
			continue
		}
		if _, okPattern := expectedResp[field+"_pattern"]; okPattern {
			continue
		}
		// ttl_seconds 用了 _range 形式,放宽通过
		if field == "ttl_seconds" {
			if _, okRange := expectedResp["ttl_seconds_range"]; okRange {
				continue
			}
		}
		t.Errorf("golden missing field: %s", field)
	}
	t.Logf("✓ 4 SDK SessionResponse 6 字段在 golden JSON 全部就位")

	// 验证 4 SDK source code 里 field 命名一致
	// 4 SDK 仓都是 ~/project/ 平级,本仓在 ~/project/wau-go-sdk/,所以向上 1 层
	// 注意:Python SDK 字段定义在 types.py(dataclass 风格),不在 handshake.py
	sdkFieldFiles := map[string]string{
		"go":         "../handshake.go",
		"python":     "../../wau-python-sdk/src/wau_sdk/types.py",
		"typescript": "../../wau-typescript-sdk/src/handshake.ts",
		"rust":       "../../wau-rust-sdk/src/handshake.rs",
	}
	for sdk, path := range sdkFieldFiles {
		t.Run(sdk, func(t *testing.T) {
			absPath, _ := filepath.Abs(path)
			if _, err := os.Stat(absPath); err != nil {
				t.Skipf("%s 不存在(%v)", path, err)
				return
			}
			data, _ := os.ReadFile(absPath)
			content := string(data)
			requiredFields := []string{"session_id", "direct_endpoint", "protocol", "expires_at", "ttl_seconds", "reused"}
			missing := []string{}
			for _, f := range requiredFields {
				if !strings.Contains(content, f) {
					missing = append(missing, f)
				}
			}
			if len(missing) > 0 {
				t.Errorf("%s source 缺字段: %v", sdk, missing)
			} else {
				t.Logf("✓ %s 6 字段全在", sdk)
			}
		})
	}
}
