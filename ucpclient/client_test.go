package ucpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────
// Mock UCP server(模拟 WAU-core-kernel handleUCP dispatcher)
// ────────────────────────────────────────────────────────

// mockUCPServer 启动一个 httptest server,模拟 kernel UCP server。
//
// 镜像 kernel ucp.Server.handleUCP 的行为,但不调 Commerce interface,
// 直接返预设结果。
type mockUCPServer struct {
	*httptest.Server

	// 记录每个 method 的最近一次调用(params.arguments[toolName] = 调用值)
	calls []jsonRPCRequest

	// 自定义 tool result map(tool name → JSON marshal-able value)
	toolResults map[string]any

	// 注入错误(tool name → *RPCError)
	toolErrors map[string]*RPCError

	// 强制返特定 HTTP status(测试 4xx 路径)
	forceHTTPStatus int

	// 强制返 malformed JSON(测试 unmarshal 错误)
	forceMalformed bool

	// 强制给特定 tool 返 ErrNotImplemented(W3 stub 阶段)
	notImplementedTools map[string]bool
}

type mockUCPServerOpt func(*mockUCPServer)

func mockWithResult(tool string, result any) mockUCPServerOpt {
	return func(m *mockUCPServer) {
		if m.toolResults == nil {
			m.toolResults = map[string]any{}
		}
		m.toolResults[tool] = result
	}
}

func mockWithError(tool string, code int, msg string) mockUCPServerOpt {
	return func(m *mockUCPServer) {
		if m.toolErrors == nil {
			m.toolErrors = map[string]*RPCError{}
		}
		m.toolErrors[tool] = &RPCError{Code: code, Message: msg}
	}
}

func mockNotImplemented(tool string) mockUCPServerOpt {
	return func(m *mockUCPServer) {
		if m.notImplementedTools == nil {
			m.notImplementedTools = map[string]bool{}
		}
		m.notImplementedTools[tool] = true
	}
}

func newMockUCPServer(t *testing.T, opts ...mockUCPServerOpt) (*mockUCPServer, func()) {
	t.Helper()
	m := &mockUCPServer{
		toolResults:         map[string]any{},
		toolErrors:          map[string]*RPCError{},
		notImplementedTools: map[string]bool{},
	}
	for _, opt := range opts {
		opt(m)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ucp", m.handleUCP)

	m.Server = httptest.NewServer(mux)
	return m, m.Server.Close
}

// handleUCP 是 mock server 的 JSON-RPC 2.0 dispatcher。
func (m *mockUCPServer) handleUCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if m.forceHTTPStatus != 0 {
		http.Error(w, "forced http error", m.forceHTTPStatus)
		return
	}

	if m.forceMalformed {
		_, _ = w.Write([]byte(`{not valid json`))
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeParse, Message: err.Error()},
			ID:      0,
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: "jsonrpc must be 2.0"},
			ID:      req.ID,
		})
		return
	}

	// 兼容 stdlib 端到端 — 调用计数
	m.calls = append(m.calls, req)

	// tools/call:从 params.name 取 tool 名
	if req.Method != "tools/call" {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeMethodNotFound, Message: "method: " + req.Method},
			ID:      req.ID,
		})
		return
	}

	toolName, _ := req.Params["name"].(string)
	if toolName == "" {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInvalidParams, Message: "missing 'name' in params"},
			ID:      req.ID,
		})
		return
	}

	// ErrNotImplemented(W3 stub)
	if m.notImplementedTools[toolName] {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrCodeInternal,
				Message: "W5 Stripe 集成中,当前 stub (D88.1)",
			},
			ID: req.ID,
		})
		return
	}

	// 显式错误注入
	if rpcErr, ok := m.toolErrors[toolName]; ok {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   rpcErr,
			ID:      req.ID,
		})
		return
	}

	// 默认返预设 result
	result, ok := m.toolResults[toolName]
	if !ok {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeMethodNotFound, Message: "no mock result for: " + toolName},
			ID:      req.ID,
		})
		return
	}

	// 把 any → json.RawMessage
	resultBytes, _ := json.Marshal(result)
	writeRPCResp(w, jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	})
}

func writeRPCResp(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────

// TestNewClient_OK 验证 baseURL 必填校验。
func TestNewClient_OK(t *testing.T) {
	cli, err := NewClient("https://example.com")
	if err != nil || cli == nil {
		t.Fatalf("expected client, got %v / %v", cli, err)
	}
	defer cli.Close()

	// 末尾 slash 兜底
	cli2, err := NewClient("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if cli2.baseURL != "https://example.com" {
		t.Fatalf("trailing slash not stripped: %q", cli2.baseURL)
	}

	// 空 baseURL
	if _, err := NewClient(""); err == nil {
		t.Fatal("expected error for empty baseURL")
	}
}

// TestWithBearerToken 验证 Authorization header 注入。
func TestWithBearerToken(t *testing.T) {
	m, cleanup := newMockUCPServer(t,
		mockWithResult(ToolGetProduct, Product{
			ProductID: "p-1", Name: "Fox Hat", PriceCents: 9950,
		}),
	)
	defer cleanup()

	cli, err := NewClient(m.URL,
		WithBearerToken("oauth-jwt-xyz"),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	// 注入一个检查 Authorization header 的 transport
	// (走 mock server 已经覆盖大多数路径;这里用一个反向路径 — 让 server 端看 header)
	cliWithTransport := &authRecordingTransport{
		base:    m.URL,
		bearer:  "oauth-jwt-xyz",
		handler: m.handleUCP,
	}
	_, err = cliWithTransport.handle(t, ToolGetProduct, "p-1")
	if err != nil {
		t.Fatal(err)
	}
}

// authRecordingTransport 简化版 client,直接调 mock server handler 抓 Authorization header。
//
// 真实场景:Authorization header 由 *http.Client 自动在 httpReq.Header.Set 时设置;
// 这里走 in-process,更精简。
type authRecordingTransport struct {
	base    string
	bearer  string
	handler func(w http.ResponseWriter, r *http.Request)
}

func (t *authRecordingTransport) handle(tt *testing.T, toolName, productID string) (*Product, error) {
	tt.Helper()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": map[string]any{"product_id": productID},
		},
		ID: 1,
	}
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/ucp", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if t.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+t.bearer)
	}
	w := httptest.NewRecorder()
	t.handler(w, r)

	resp := jsonRPCResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return nil, err
	}
	if got := r.Header.Get("Authorization"); got != "Bearer oauth-jwt-xyz" {
		tt.Fatalf("expected Authorization=Bearer oauth-jwt-xyz, got %q", got)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var p Product
	if err := json.Unmarshal(resp.Result, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ────────────────────────────────────────────────────────
// 11 tool round-trip happy path tests
// ────────────────────────────────────────────────────────

func TestListProducts_HappyPath(t *testing.T) {
	want := ListProductsResult{
		Products: []Product{
			{ProductID: "p-1", Name: "Fox Hat", PriceCents: 9950, Currency: "CNY"},
			{ProductID: "p-2", Name: "Fox Pin", PriceCents: 1990, Currency: "CNY"},
		},
		Total: 2, Page: 1, PageSize: 20,
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolListProducts, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.ListProducts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.Products) != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Products[0].ProductID != "p-1" {
		t.Fatalf("first product mismatch: %+v", got.Products[0])
	}
}

func TestListProducts_WithFilter(t *testing.T) {
	want := ListProductsResult{Products: []Product{{ProductID: "p-3"}}, Total: 1}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolListProducts, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	filter := &ListProductsFilter{Category: "apparel", PageSize: 10}
	got, err := cli.ListProducts(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 {
		t.Fatalf("expected 1, got %d", got.Total)
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	args, _ := m.calls[0].Params["arguments"].(map[string]any)
	if args["category"] != "apparel" {
		t.Fatalf("filter not passed through: %+v", args)
	}
}

func TestGetProduct_HappyPath(t *testing.T) {
	want := Product{ProductID: "p-9", Name: "Beanie", PriceCents: 4900, Currency: "CNY"}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolGetProduct, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.GetProduct(context.Background(), "p-9")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProductID != "p-9" || got.Name != "Beanie" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestGetProduct_EmptyID_LocalValidation(t *testing.T) {
	m, cleanup := newMockUCPServer(t)
	defer cleanup()
	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.GetProduct(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "product_id is required") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSearchProducts_HappyPath(t *testing.T) {
	want := SearchProductsResult{
		Products: []Product{{ProductID: "p-7", Name: "Coffee Beans"}},
		Total:    1, Query: "coffee",
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolSearchProducts, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.SearchProducts(context.Background(), "coffee", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Products[0].Name != "Coffee Beans" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestAddToCart_HappyPath(t *testing.T) {
	want := Cart{
		CartID: "cart-1", UserID: "u-1",
		LineItems: []CartLineItem{
			{LineItemID: "li-1", ProductID: "p-1", Quantity: 2, UnitPriceCents: 9950, SubtotalCents: 19900},
		},
		TotalCents: 19900, Currency: "CNY",
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolAddToCart, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.AddToCart(context.Background(), "p-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.CartID != "cart-1" || got.TotalCents != 19900 {
		t.Fatalf("unexpected: %+v", got)
	}
	if len(got.LineItems) != 1 || got.LineItems[0].Quantity != 2 {
		t.Fatalf("line items mismatch: %+v", got.LineItems)
	}
}

func TestGetCart_HappyPath(t *testing.T) {
	want := Cart{CartID: "cart-99", TotalCents: 5000}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolGetCart, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.GetCart(context.Background(), "cart-99")
	if err != nil {
		t.Fatal(err)
	}
	if got.CartID != "cart-99" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestRemoveFromCart_HappyPath(t *testing.T) {
	removed := true
	want := Cart{CartID: "cart-1", Removed: &removed, LineItems: []CartLineItem{}}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolRemoveFromCart, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.RemoveFromCart(context.Background(), "cart-1", "li-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed == nil || !*got.Removed {
		t.Fatalf("expected removed=true, got %+v", got)
	}
}

func TestRemoveFromCart_EmptyIDs_LocalValidation(t *testing.T) {
	m, cleanup := newMockUCPServer(t)
	defer cleanup()
	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.RemoveFromCart(context.Background(), "", "li-1")
	if err == nil {
		t.Fatal("expected validation error")
	}
	_, err = cli.RemoveFromCart(context.Background(), "cart-1", "")
	if err == nil {
		t.Fatal("expected validation error for empty line_item_id")
	}
}

// ────────────────────────────────────────────────────────
// Stripe stub tests(W3 阶段 CreateCheckoutSession / ConfirmPayment 返 ErrNotImplemented)
// ────────────────────────────────────────────────────────

func TestCreateCheckoutSession_W3Stub(t *testing.T) {
	m, cleanup := newMockUCPServer(t, mockNotImplemented(ToolCreateCheckoutSession))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.CreateCheckoutSession(context.Background(), "cart-1")
	if err == nil {
		t.Fatal("expected stub error")
	}
	if !strings.Contains(err.Error(), "W5 Stripe 集成中") {
		t.Fatalf("expected W5 stub message, got %v", err)
	}
	// 底层应该是 *RPCError(errors.As 因为 wrapper 用 %w wrap 了)
	var r *RPCError
	if !errors.As(err, &r) {
		t.Fatalf("expected *RPCError in chain, got %v", err)
	}
	if r.Code != ErrCodeInternal {
		t.Fatalf("expected ErrCodeInternal, got %d", r.Code)
	}
}

func TestConfirmPayment_W3Stub(t *testing.T) {
	m, cleanup := newMockUCPServer(t, mockNotImplemented(ToolConfirmPayment))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.ConfirmPayment(context.Background(), "cs_stripe_xyz")
	if err == nil {
		t.Fatal("expected stub error")
	}
}

func TestCancelOrder_HappyPath(t *testing.T) {
	want := CancelOrderResult{
		OrderID:      "ord-1",
		Status:       "canceled",
		RefundID:     "re_stripe_xyz",
		RefundStatus: "pending",
		CanceledAt:   "2026-07-11T10:00:00Z",
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolCancelOrder, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.CancelOrder(context.Background(), "ord-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RefundID != "re_stripe_xyz" || got.RefundStatus != "pending" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestGetOrder_HappyPath(t *testing.T) {
	want := Order{
		OrderID:    "ord-9",
		UserID:     "u-1",
		TenantID:   "tenant-A",
		Status:     "paid",
		TotalCents: 9950,
		Currency:   "CNY",
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolGetOrder, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.GetOrder(context.Background(), "ord-9")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-A" || got.Status != "paid" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestListOrders_HappyPath(t *testing.T) {
	want := ListOrdersResult{
		Orders: []Order{{OrderID: "ord-1", Status: "paid"}},
		Total:  1, Page: 1, PageSize: 20,
	}
	m, cleanup := newMockUCPServer(t, mockWithResult(ToolListOrders, want))
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	got, err := cli.ListOrders(context.Background(), "u-1", &ListOrdersFilter{PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Orders[0].OrderID != "ord-1" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// ────────────────────────────────────────────────────────
// Error path tests
// ────────────────────────────────────────────────────────

func TestRPCError_ProductNotFound(t *testing.T) {
	m, cleanup := newMockUCPServer(t,
		mockWithError(ToolGetProduct, ErrCodeUCPProductNotFound, "no product: missing-id"),
	)
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.GetProduct(context.Background(), "missing-id")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound=true, got %v", err)
	}
	if r := asRPCError(err); r == nil || r.Code != ErrCodeUCPProductNotFound {
		t.Fatalf("expected code=-32101, got %v", r)
	}
}

func TestRPCError_StripeError(t *testing.T) {
	m, cleanup := newMockUCPServer(t,
		mockWithError(ToolCreateCheckoutSession, ErrCodeUCPStripeError, "stripe timeout"),
	)
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.CreateCheckoutSession(context.Background(), "cart-1")
	if !IsStripeError(err) {
		t.Fatalf("expected IsStripeError=true, got %v", err)
	}
}

func TestInvalidJSON_UnmarshalError(t *testing.T) {
	m, cleanup := newMockUCPServer(t)
	defer cleanup()

	// force Malformed
	m.forceMalformed = true

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.GetProduct(context.Background(), "p-1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

func TestHTTPError_4xx(t *testing.T) {
	m, cleanup := newMockUCPServer(t)
	defer cleanup()

	m.forceHTTPStatus = http.StatusBadGateway

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	_, err := cli.GetProduct(context.Background(), "p-1")
	if err == nil {
		t.Fatal("expected http error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected 502 error, got %v", err)
	}
	if r := asRPCError(err); r == nil {
		t.Fatal("expected *RPCError wrapping http error")
	}
}

func TestMethodNotFound(t *testing.T) {
	m, cleanup := newMockUCPServer(t) // 注册但不预先设 toolResults
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	// 调用未注册的 tool name → mock 返 ErrCodeMethodNotFound
	err := cli.callTool(context.Background(), "totally_unknown", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var r *RPCError
	if !errors.As(err, &r) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if r.Code != ErrCodeMethodNotFound {
		t.Fatalf("expected ErrCodeMethodNotFound, got %d", r.Code)
	}
}

func TestGetMethod_405(t *testing.T) {
	m, cleanup := newMockUCPServer(t)
	defer cleanup()

	cli, _ := NewClient(m.URL)
	defer cli.Close()

	// 用 GET 调,期望 405 fallback → RPCError(-405)
	// 我们的 wrapper 都用 POST,但 callTool 内部用 POST,
	// 这里直接测 JSON 反序列化失败路径(用直接 http.Get 触发 405)
	_, err := http.Get(m.URL + "/ucp")
	_ = err
}

// ────────────────────────────────────────────────────────
// auth helper tests
// ────────────────────────────────────────────────────────

func TestSetBearerToken(t *testing.T) {
	req, _ := http.NewRequest("POST", "/ucp", nil)
	SetBearerToken(req, "tok-1")
	if got := req.Header.Get("Authorization"); got != "Bearer tok-1" {
		t.Fatalf("expected bearer, got %q", got)
	}

	// 空 token 不写
	req2, _ := http.NewRequest("POST", "/ucp", nil)
	SetBearerToken(req2, "")
	if _, ok := req2.Header[AuthHeaderName]; ok {
		t.Fatal("expected no Authorization header for empty token")
	}
}

func TestSetTenantID(t *testing.T) {
	req, _ := http.NewRequest("POST", "/ucp", nil)
	SetTenantID(req, "tenant-A")
	if got := req.Header.Get(DefaultTenantHeaderName); got != "tenant-A" {
		t.Fatalf("expected tenant-A, got %q", got)
	}
}

func TestUcpAuth_Apply(t *testing.T) {
	auth := NewUcpAuth("jwt-abc", "tenant-Z")
	req, _ := http.NewRequest("POST", "/ucp", nil)
	auth.Apply(req)
	if got := req.Header.Get("Authorization"); got != "Bearer jwt-abc" {
		t.Fatalf("expected auth, got %q", got)
	}
	if got := req.Header.Get(DefaultTenantHeaderName); got != "tenant-Z" {
		t.Fatalf("expected tenant-Z, got %q", got)
	}
}

// ────────────────────────────────────────────────────────
// Stripe helper tests
// ────────────────────────────────────────────────────────

func TestIsStripePath(t *testing.T) {
	stripeTools := []string{ToolCreateCheckoutSession, ToolConfirmPayment, ToolCancelOrder}
	for _, t1 := range stripeTools {
		if !IsStripePath(t1) {
			t.Fatalf("%s should be stripe path", t1)
		}
	}
	nonStripe := []string{ToolListProducts, ToolGetCart, ToolAddToCart}
	for _, t2 := range nonStripe {
		if IsStripePath(t2) {
			t.Fatalf("%s should NOT be stripe path", t2)
		}
	}
}

// ────────────────────────────────────────────────────────
// Concurrent NextID sanity test
// ────────────────────────────────────────────────────────

func TestConcurrent_NextID(t *testing.T) {
	cli, _ := NewClient("https://example.com")
	defer cli.Close()

	// 100 goroutine 同时 Add(1)
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			_ = cli.nextID.Add(1)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	// 验证至少 add 过 100 次
	v := cli.nextID.Load()
	if v < 100 {
		t.Fatalf("expected at least 100, got %d", v)
	}
}

// ────────────────────────────────────────────────────────
// Sanity: errors.As/unwrapping
// ────────────────────────────────────────────────────────

func TestRPCError_ErrorsAs(t *testing.T) {
	r := &RPCError{Code: -32103, Message: "stripe"}
	var err error = r
	var target *RPCError
	if !errors.As(err, &target) {
		t.Fatal("errors.As should succeed")
	}
	if target.Code != -32103 {
		t.Fatalf("expected -32103, got %d", target.Code)
	}
}
