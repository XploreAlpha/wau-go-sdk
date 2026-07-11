package ucpclient

import (
	"context"
	"fmt"
)

// 11 commerce tool name 常量(对齐 kernel ucp/server.go ToolXxx + handler routeToCommerce)。
//
// 公开:SDK caller 可以用 const 拼 params,也可用 typed wrapper method。
const (
	ToolListProducts          = "list_products"
	ToolGetProduct            = "get_product"
	ToolSearchProducts        = "search_products"
	ToolAddToCart             = "add_to_cart"
	ToolGetCart               = "get_cart"
	ToolRemoveFromCart        = "remove_from_cart"
	ToolCreateCheckoutSession = "create_checkout_session"
	ToolConfirmPayment        = "confirm_payment"
	ToolGetOrder              = "get_order"
	ToolListOrders            = "list_orders"
	ToolCancelOrder           = "cancel_order"
)

// ────────────────────────────────────────────────────────
// 11 commerce tool wrapper(D88.5 W3 实装)
// ────────────────────────────────────────────────────────

// ListProducts 调 list_products tool。
//
// filter 可为 nil(列所有)。返回 *ListProductsResult。
func (c *Client) ListProducts(ctx context.Context, filter *ListProductsFilter) (*ListProductsResult, error) {
	var out ListProductsResult
	if err := c.callTool(ctx, ToolListProducts, filter, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProduct 调 get_product tool(product_id 必填)。
func (c *Client) GetProduct(ctx context.Context, productID string) (*Product, error) {
	if productID == "" {
		return nil, fmt.Errorf("ucpclient: product_id is required")
	}
	args := map[string]any{"product_id": productID}
	var out Product
	if err := c.callTool(ctx, ToolGetProduct, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchProducts 调 search_products tool(query 必填,limit 可选默认 10)。
func (c *Client) SearchProducts(ctx context.Context, query string, limit int) (*SearchProductsResult, error) {
	if query == "" {
		return nil, fmt.Errorf("ucpclient: query is required")
	}
	args := map[string]any{"query": query}
	if limit > 0 {
		args["limit"] = limit
	}
	var out SearchProductsResult
	if err := c.callTool(ctx, ToolSearchProducts, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddToCart 调 add_to_cart tool(product_id + quantity 必填;quantity 默认 1)。
func (c *Client) AddToCart(ctx context.Context, productID string, quantity int) (*Cart, error) {
	if productID == "" {
		return nil, fmt.Errorf("ucpclient: product_id is required")
	}
	if quantity <= 0 {
		quantity = 1
	}
	args := map[string]any{
		"product_id": productID,
		"quantity":   quantity,
	}
	var out Cart
	if err := c.callTool(ctx, ToolAddToCart, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCart 调 get_cart tool(cart_id 必填)。
func (c *Client) GetCart(ctx context.Context, cartID string) (*Cart, error) {
	if cartID == "" {
		return nil, fmt.Errorf("ucpclient: cart_id is required")
	}
	args := map[string]any{"cart_id": cartID}
	var out Cart
	if err := c.callTool(ctx, ToolGetCart, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveFromCart 调 remove_from_cart tool(cart_id + line_item_id 必填)。
func (c *Client) RemoveFromCart(ctx context.Context, cartID, lineItemID string) (*Cart, error) {
	if cartID == "" {
		return nil, fmt.Errorf("ucpclient: cart_id is required")
	}
	if lineItemID == "" {
		return nil, fmt.Errorf("ucpclient: line_item_id is required")
	}
	args := map[string]any{
		"cart_id":      cartID,
		"line_item_id": lineItemID,
	}
	var out Cart
	if err := c.callTool(ctx, ToolRemoveFromCart, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateCheckoutSession 调 create_checkout_session tool(cart_id 必填 — W5+ Stripe 集成)。
//
// W3 stub 阶段:kernel handler 返 ErrNotImplemented 通过 errors.Is(err, ErrNotImplemented);
// W5 阶段:返 *CheckoutSession(checkout_url 由 Stripe Checkout 生成)。
func (c *Client) CreateCheckoutSession(ctx context.Context, cartID string) (*CheckoutSession, error) {
	if cartID == "" {
		return nil, fmt.Errorf("ucpclient: cart_id is required")
	}
	args := map[string]any{"cart_id": cartID}
	var out CheckoutSession
	if err := c.callTool(ctx, ToolCreateCheckoutSession, args, &out); err != nil {
		// W3 stub 友好提示(W5 Stripe 真接入后,正常路径返 *CheckoutSession)
		if r := asRPCError(err); r != nil && r.Code == ErrCodeInternal {
			return nil, fmt.Errorf("ucpclient: create_checkout_session (W5 Stripe 集成中): %w", err)
		}
		return nil, err
	}
	return &out, nil
}

// ConfirmPayment 调 confirm_payment tool(checkout_session_id 必填 — W5+ Stripe payment_intent)。
//
// W3 stub 阶段:同 CreateCheckoutSession,W5+ 返 *PaymentConfirmation。
func (c *Client) ConfirmPayment(ctx context.Context, checkoutSessionID string) (*PaymentConfirmation, error) {
	if checkoutSessionID == "" {
		return nil, fmt.Errorf("ucpclient: checkout_session_id is required")
	}
	args := map[string]any{"checkout_session_id": checkoutSessionID}
	var out PaymentConfirmation
	if err := c.callTool(ctx, ToolConfirmPayment, args, &out); err != nil {
		if r := asRPCError(err); r != nil && r.Code == ErrCodeInternal {
			return nil, fmt.Errorf("ucpclient: confirm_payment (W5 Stripe 集成中): %w", err)
		}
		return nil, err
	}
	return &out, nil
}

// GetOrder 调 get_order tool(order_id 必填)。
func (c *Client) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	if orderID == "" {
		return nil, fmt.Errorf("ucpclient: order_id is required")
	}
	args := map[string]any{"order_id": orderID}
	var out Order
	if err := c.callTool(ctx, ToolGetOrder, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListOrders 调 list_orders tool(user_id 必填,filter 可选)。
func (c *Client) ListOrders(ctx context.Context, userID string, filter *ListOrdersFilter) (*ListOrdersResult, error) {
	if userID == "" {
		return nil, fmt.Errorf("ucpclient: user_id is required")
	}
	args := map[string]any{"user_id": userID}
	if filter != nil {
		args["filter"] = filter
	}
	var out ListOrdersResult
	if err := c.callTool(ctx, ToolListOrders, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelOrder 调 cancel_order tool(order_id 必填;W5+ 走 Stripe refund)。
func (c *Client) CancelOrder(ctx context.Context, orderID string) (*CancelOrderResult, error) {
	if orderID == "" {
		return nil, fmt.Errorf("ucpclient: order_id is required")
	}
	args := map[string]any{"order_id": orderID}
	var out CancelOrderResult
	if err := c.callTool(ctx, ToolCancelOrder, args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
