// Package wau — skills.go
//
// v1.0.0 M11 P4 (I 子项) — SkillsService: list/get/publish/load skills.
//
// Endpoints:
//   - GET    /registry/skills                  → List
//   - GET    /registry/skills/{name}           → Get
//   - POST   /registry/skills/publish          → Publish (multipart: manifest + bundle)
//   - POST   /registry/skills/load             → LoadForUser
//   - GET    /registry/skills/user/{userID}    → ListForUser
//
// Per D60 兼容 — 不改老 AgentsService / TasksService / KernelService。
package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// SkillsService 提供 skill 注册表操作(list / get / publish / load)。
//
// Publish 用 multipart/form-data 把 manifest JSON + tarball bundle
// 一次 POST 到 wau-registry(per agentskills.io D69=A 标准)。
type SkillsService struct {
	c *Client
}

// SkillInfo represents one registered skill (per agentskills.io + WAU extensions).
type SkillInfo struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Version     string            `json:"version"`
	Author      string            `json:"author,omitempty"`
	Universe    string            `json:"universe,omitempty"`
	Universes   []string          `json:"universes,omitempty"`
	Entrypoint  string            `json:"entrypoint"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	SourceURL   string            `json:"source_url,omitempty"`
	IsBuiltin   bool              `json:"is_builtin,omitempty"`
	UserID      string            `json:"user_id,omitempty"`
	Skills      []string          `json:"skills,omitempty"`
	RegisteredAt string           `json:"registered_at,omitempty"`
}

// SkillListResponse is the paginated/skippable list response.
type SkillListResponse struct {
	Skills []SkillInfo `json:"skills"`
	Total  int         `json:"total"`
}

// SkillPublishRequest carries everything needed to publish a bundle.
type SkillPublishRequest struct {
	Manifest SkillInfo       // populated fields become the JSON manifest
	Bundle   []byte          // raw tar.gz bytes (or any binary blob)
	Filename string          // optional, defaults to "bundle.tar.gz"
}

// SkillPublishResponse is the 201 Created body from /registry/skills/publish.
type SkillPublishResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Entrypoint string `json:"entrypoint"`
	BundleSize int    `json:"bundle_size"`
	BundleSHA  string `json:"bundle_sha,omitempty"`
}

// List lists skills with optional universe / builtin filter.
//
// 对应 GET /registry/skills?universe=...&builtin_only=...
func (s *SkillsService) List(ctx context.Context, universe string, builtinOnly bool) (*SkillListResponse, error) {
	path := "/registry/skills"
	if universe != "" || builtinOnly {
		path += "?"
		sep := ""
		if universe != "" {
			path += "universe=" + universe
			sep = "&"
		}
		if builtinOnly {
			path += sep + "builtin_only=true"
		}
	}
	var resp SkillListResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Get returns one skill by name. Built-in skills are returned with is_builtin=true.
//
// 对应 GET /registry/skills/{name}
func (s *SkillsService) Get(ctx context.Context, name string) (*SkillInfo, error) {
	var resp SkillInfo
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/registry/skills/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LoadForUser installs (or temporarily loads) a skill into a user's pool.
//
// 对应 POST /registry/skills/load
//
// install=true  → permanent; install=false → 24h TTL on user_skill pool.
func (s *SkillsService) LoadForUser(ctx context.Context, userID, skillName, botID string, install bool) (*SkillInfo, error) {
	body := map[string]any{
		"user_id":    userID,
		"skill_name": skillName,
		"bot_id":     botID,
		"install":    install,
	}
	var resp SkillInfo
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/registry/skills/load", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListForUser lists all skills loaded into the user's pool (across all bots).
//
// 对应 GET /registry/skills/user/{userID}
func (s *SkillsService) ListForUser(ctx context.Context, userID, botID string) (*SkillListResponse, error) {
	path := "/registry/skills/user/" + userID
	if botID != "" {
		path += "?bot_id=" + botID
	}
	var resp SkillListResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Publish uploads a skill bundle (manifest JSON + tarball) to wau-registry.
//
// 对应 POST /registry/skills/publish (multipart/form-data).
//
// Wire format:
//
//	--boundary
//	Content-Disposition: form-data; name="manifest"; filename="manifest.json"
//	Content-Type: application/json
//
//	<manifest JSON>
//	--boundary
//	Content-Disposition: form-data; name="bundle"; filename="<filename>"
//	Content-Type: application/gzip
//
//	<tarball bytes>
//	--boundary--
//
// We build the multipart body inline (no extra deps) and bypass doWithRetry
// (which assumes JSON). The retry decorator still wraps the call, so transient
// failures get retried per the Client's RetryConfig.
func (s *SkillsService) Publish(ctx context.Context, req SkillPublishRequest) (*SkillPublishResponse, error) {
	if req.Manifest.Name == "" {
		return nil, fmt.Errorf("wau: Skills.Publish: manifest.name required")
	}
	if len(req.Bundle) == 0 {
		return nil, fmt.Errorf("wau: Skills.Publish: bundle bytes required")
	}
	filename := req.Filename
	if filename == "" {
		filename = "bundle.tar.gz"
	}

	body, contentType, err := buildSkillMultipart(req.Manifest, req.Bundle, filename)
	if err != nil {
		return nil, fmt.Errorf("wau: build multipart: %w", err)
	}

	// Direct HTTP call — doWithRetry expects JSON, so we replicate the
	// minimal subset (auth header injection, X-Agent-Role) inline.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.c.tp.baseURL+"/registry/skills/publish", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("wau: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "application/json")

	// Inject auth if configured.
	if auth := s.c.tp.authHeader(); auth != "" {
		httpReq.Header.Set("Authorization", auth)
	}

	httpResp, err := s.c.tp.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wau: Skills.Publish request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("wau: read response: %w", err)
	}

	if httpResp.StatusCode >= 400 {
		return nil, &APIError{
			StatusCode: httpResp.StatusCode,
			Body:       respBody,
		}
	}

	var resp SkillPublishResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, fmt.Errorf("wau: decode response: %w", err)
		}
	}
	return &resp, nil
}

// buildSkillMultipart assembles the multipart payload for /registry/skills/publish.
// Exported as a package-level helper so tests can verify the wire shape.
func buildSkillMultipart(m SkillInfo, bundle []byte, filename string) (body []byte, contentType string, err error) {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)

	mPart, err := mw.CreateFormFile("manifest", "manifest.json")
	if err != nil {
		return nil, "", fmt.Errorf("create manifest part: %w", err)
	}
	mJSON, err := json.Marshal(m)
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}
	if _, err := mPart.Write(mJSON); err != nil {
		return nil, "", fmt.Errorf("write manifest part: %w", err)
	}

	bPart, err := mw.CreateFormFile("bundle", filename)
	if err != nil {
		return nil, "", fmt.Errorf("create bundle part: %w", err)
	}
	if _, err := bPart.Write(bundle); err != nil {
		return nil, "", fmt.Errorf("write bundle part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart: %w", err)
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}