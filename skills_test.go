// Package wau — skills_test.go
//
// v1.0.0 M11 P4 (I 子项) SkillsService unit tests.
//
// Verifies:
//   - buildSkillMultipart produces well-formed multipart payload
//   - Publish posts to /registry/skills/publish with correct Content-Type
//   - Server errors surface as *APIError
//   - List / Get / LoadForUser round-trip
package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newMultipartReader is a tiny helper for parsing multipart bodies in tests.
func newMultipartReader(contentType string, body []byte) (*multipart.Reader, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	return multipart.NewReader(bytes.NewReader(body), params["boundary"]), nil
}

func TestBuildSkillMultipart(t *testing.T) {
	m := SkillInfo{
		Name:       "weather-bot",
		Version:    "0.1.0",
		Entrypoint: "skills/weather/main.py",
	}
	bundle := []byte("fake-tarball-bytes")

	body, ct, err := buildSkillMultipart(m, bundle, "bundle.tar.gz")
	if err != nil {
		t.Fatalf("buildSkillMultipart: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("media=%q, want multipart/form-data", mediaType)
	}
	if params["boundary"] == "" {
		t.Error("missing boundary")
	}

	// Parse the multipart body.
	reader, err := newMultipartReader(ct, body)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	var gotManifest string
	var gotBundle []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		buf, _ := io.ReadAll(part)
		switch part.FormName() {
		case "manifest":
			gotManifest = string(buf)
		case "bundle":
			gotBundle = buf
		}
	}
	if gotManifest == "" {
		t.Fatal("manifest part missing")
	}
	if string(gotBundle) != string(bundle) {
		t.Errorf("bundle bytes mismatch: %q vs %q", gotBundle, bundle)
	}
	var m2 SkillInfo
	if err := json.Unmarshal([]byte(gotManifest), &m2); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if m2.Name != "weather-bot" {
		t.Errorf("manifest.Name=%q", m2.Name)
	}
}

func TestSkillsPublish_RoundTrip(t *testing.T) {
	var gotPath, gotContentType string
	var gotBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SkillPublishResponse{
			Name:       "weather-bot",
			Version:    "0.1.0",
			Entrypoint: "skills/weather/main.py",
			BundleSize: len(gotBody),
		})
	}))
	defer ts.Close()

	c, err := New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Skills().Publish(context.Background(), SkillPublishRequest{
		Manifest: SkillInfo{
			Name:       "weather-bot",
			Version:    "0.1.0",
			Entrypoint: "skills/weather/main.py",
		},
		Bundle: []byte("dummy-bytes"),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.Name != "weather-bot" {
		t.Errorf("resp.Name=%q", resp.Name)
	}
	if gotPath != "/registry/skills/publish" {
		t.Errorf("path=%q", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type=%q", gotContentType)
	}
}

func TestSkillsPublish_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "validation failed", 400)
	}))
	defer ts.Close()

	c, _ := New(ts.URL)
	_, err := c.Skills().Publish(context.Background(), SkillPublishRequest{
		Manifest: SkillInfo{Name: "x", Entrypoint: "main.py"},
		Bundle:   []byte("x"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status=%d", apiErr.StatusCode)
	}
}

func TestSkillsPublish_ValidationErrors(t *testing.T) {
	c, _ := New("http://localhost:1")
	ctx := context.Background()

	// Missing manifest name.
	_, err := c.Skills().Publish(ctx, SkillPublishRequest{
		Bundle: []byte("x"),
	})
	if err == nil || !strings.Contains(err.Error(), "manifest.name") {
		t.Errorf("expected name-required error, got %v", err)
	}

	// Missing bundle bytes.
	_, err = c.Skills().Publish(ctx, SkillPublishRequest{
		Manifest: SkillInfo{Name: "x", Entrypoint: "main.py"},
	})
	if err == nil || !strings.Contains(err.Error(), "bundle bytes") {
		t.Errorf("expected bundle-required error, got %v", err)
	}
}

func TestSkillsList_Get_LoadForUser(t *testing.T) {
	// Mock /registry/skills (list + get + load + user list).
	mux := http.NewServeMux()
	mux.HandleFunc("/registry/skills", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(SkillListResponse{
				Skills: []SkillInfo{{Name: "weather", Version: "0.1.0", IsBuiltin: true}},
				Total:  1,
			})
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/registry/skills/weather", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SkillInfo{Name: "weather", Version: "0.1.0", IsBuiltin: true})
	})
	mux.HandleFunc("/registry/skills/load", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SkillInfo{Name: "weather", Version: "0.1.0"})
	})
	mux.HandleFunc("/registry/skills/user/u1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SkillListResponse{Skills: []SkillInfo{{Name: "weather"}}, Total: 1})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, _ := New(ts.URL)
	ctx := context.Background()

	// List.
	lst, err := c.Skills().List(ctx, "", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if lst.Total != 1 || lst.Skills[0].Name != "weather" {
		t.Errorf("List=%+v", lst)
	}

	// Get.
	sk, err := c.Skills().Get(ctx, "weather")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !sk.IsBuiltin {
		t.Errorf("Get IsBuiltin=false")
	}

	// LoadForUser.
	loaded, err := c.Skills().LoadForUser(ctx, "u1", "weather", "bot-a", true)
	if err != nil {
		t.Fatalf("LoadForUser: %v", err)
	}
	if loaded.Name != "weather" {
		t.Errorf("loaded=%+v", loaded)
	}

	// ListForUser.
	ulst, err := c.Skills().ListForUser(ctx, "u1", "")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if ulst.Total != 1 {
		t.Errorf("ListForUser=%+v", ulst)
	}
}