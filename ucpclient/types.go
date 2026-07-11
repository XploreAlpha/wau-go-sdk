package ucpclient

// ────────────────────────────────────────────────────────
// 8 commerce DTO(per [[process/2026-07-11-W3-UCP-client-SDK-design]] §三 + kernel ucp/commerce_mock.go)
//
// JSON 字段 byte-equal:跟 kernel ucp.Commerce interface 返的 any 期望 shape 对齐;
// D13 byte-equal 跨 5 SDK 由 design doc §三 8 DTO 详设保证。
// ────────────────────────────────────────────────────────

// Product 是商品 DTO(对应 tool 1-3: list_products / get_product / search_products)。
type Product struct {
	ProductID   string   `json:"product_id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	PriceCents  int64    `json:"price_cents,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Stock       int      `json:"stock,omitempty"`
	Images      []string `json:"images,omitempty"`
	Category    string   `json:"category,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	// 可选字段:per design doc §三.3.1
	Available *bool  `json:"available,omitempty"`
	SKU       string `json:"sku,omitempty"`
}

// ListProductsFilter 是 list_products 可选过滤参数。
type ListProductsFilter struct {
	Category      string `json:"category,omitempty"`
	PriceMinCents int64  `json:"price_min_cents,omitempty"`
	PriceMaxCents int64  `json:"price_max_cents,omitempty"`
	Page          int    `json:"page,omitempty"`
	PageSize      int    `json:"page_size,omitempty"`
}

// ListProductsResult 是 list_products 返的 DTO。
type ListProductsResult struct {
	Products []Product `json:"products"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// SearchProductsResult 是 search_products 返的 DTO(简化版,无 page/page_size)。
type SearchProductsResult struct {
	Products []Product `json:"products"`
	Total    int       `json:"total"`
	Query    string    `json:"query,omitempty"`
}

// CartLineItem 是购物车单项(对应 tool 4-6)。
type CartLineItem struct {
	LineItemID     string `json:"line_item_id,omitempty"`
	ProductID      string `json:"product_id,omitempty"`
	Name           string `json:"name,omitempty"`
	Quantity       int    `json:"quantity,omitempty"`
	UnitPriceCents int64  `json:"unit_price_cents,omitempty"`
	SubtotalCents  int64  `json:"subtotal_cents,omitempty"`
}

// Cart 是购物车 DTO(对应 tool 4-6: add_to_cart / get_cart / remove_from_cart)。
type Cart struct {
	CartID      string         `json:"cart_id,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	TenantID    string         `json:"tenant_id,omitempty"` // per D65 multi-tenant
	LineItems   []CartLineItem `json:"line_items,omitempty"`
	TotalCents  int64          `json:"total_cents,omitempty"`
	Currency    string         `json:"currency,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
	ExpiresAt   string         `json:"expires_at,omitempty"` // 24h 默认,per UCP spec
	LastUpdated string         `json:"last_updated,omitempty"`
	// remove_from_cart 特有
	Removed *bool `json:"removed,omitempty"`
}

// CheckoutSession 是 Stripe Checkout Session DTO(tool 7: create_checkout_session)。
//
// W5+ Stripe 集成时,kernel 通过 /v1/ucp/webhooks/stripe 调 SDK,
// SDK 0 直接 Stripe(透明)。
type CheckoutSession struct {
	CheckoutSessionID string `json:"checkout_session_id,omitempty"`
	CartID            string `json:"cart_id,omitempty"`
	CheckoutURL       string `json:"checkout_url,omitempty"`
	AmountCents       int64  `json:"amount_cents,omitempty"`
	Currency          string `json:"currency,omitempty"`
	Status            string `json:"status,omitempty"` // "pending" / "completed" / "expired"
	ExpiresAt         string `json:"expires_at,omitempty"`
}

// PaymentConfirmation 是 Stripe payment_intent 确认 DTO(tool 8: confirm_payment)。
type PaymentConfirmation struct {
	CheckoutSessionID string `json:"checkout_session_id,omitempty"`
	PaymentIntentID   string `json:"payment_intent_id,omitempty"`
	Status            string `json:"status,omitempty"` // "succeeded" / "failed" / "processing"
	OrderID           string `json:"order_id,omitempty"`
}

// Order 是订单 DTO(tool 9-10-11: get_order / list_orders / cancel_order)。
//
// 必含 tenant_id(per D65 multi-tenant)。
type Order struct {
	OrderID         string         `json:"order_id,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	TenantID        string         `json:"tenant_id,omitempty"` // per D65
	Status          string         `json:"status,omitempty"`    // "pending" / "paid" / "shipped" / "delivered" / "canceled" / "refunded"
	LineItems       []CartLineItem `json:"line_items,omitempty"`
	TotalCents      int64          `json:"total_cents,omitempty"`
	Currency        string         `json:"currency,omitempty"`
	ShippingAddress map[string]any `json:"shipping_address,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
}

// ListOrdersFilter 是 list_orders 可选过滤参数。
type ListOrdersFilter struct {
	Status   []string `json:"status,omitempty"`
	DateFrom string   `json:"date_from,omitempty"`
	DateTo   string   `json:"date_to,omitempty"`
	Page     int      `json:"page,omitempty"`
	PageSize int      `json:"page_size,omitempty"`
}

// ListOrdersResult 是 list_orders 返的包装 DTO。
type ListOrdersResult struct {
	Orders   []Order `json:"orders"`
	Total    int     `json:"total"`
	Page     int     `json:"page,omitempty"`
	PageSize int     `json:"page_size,omitempty"`
}

// CancelOrderResult 是 cancel_order 返的 DTO(含 Stripe refund 流程)。
type CancelOrderResult struct {
	OrderID      string `json:"order_id,omitempty"`
	Status       string `json:"status,omitempty"` // 通常 "canceled"
	RefundID     string `json:"refund_id,omitempty"`
	RefundStatus string `json:"refund_status,omitempty"` // "pending" / "succeeded" / "failed"
	CanceledAt   string `json:"canceled_at,omitempty"`
	RefundReason string `json:"refund_reason,omitempty"`
}
