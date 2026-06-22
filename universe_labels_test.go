// v0.8.0 M3-2A Universe Labels 校验函数测试
//
// 跟 afp-protocol tests/unit/universe_labels.test.ts 语义对齐
// 跟 WAU-core-kernel internal/registry/universe_labels_test.go 语义对齐
package wau

import (
	"strings"
	"testing"
)

// =============================================================================
// backward compat (老 client 不传 labels)
// =============================================================================

func TestValidateUniverseLabels_NilOrEmpty(t *testing.T) {
	r := ValidateUniverseLabels(nil)
	if !r.OK || len(r.Warnings) != 0 || len(r.Errors) != 0 {
		t.Errorf("nil labels: expected OK, got %+v", r)
	}
	r = ValidateUniverseLabels(map[string]string{})
	if !r.OK {
		t.Errorf("empty labels: expected OK, got %+v", r)
	}
}

// =============================================================================
// 6 reserved labels 各测
// =============================================================================

func TestValidateUniverseLabels_ReservedRegion(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"region": "cn-shanghai"})
	if !r.OK || len(r.Warnings) != 0 {
		t.Errorf("region=cn-shanghai should be OK, got %+v", r)
	}
}

func TestValidateUniverseLabels_ReservedGPU(t *testing.T) {
	cases := []struct {
		value     string
		warningCt int
	}{
		{"true", 0},
		{"false", 0},
		{"yes", 1}, // 不在 enum
		{"", 1},    // 空 value
	}
	for _, c := range cases {
		r := ValidateUniverseLabels(map[string]string{"gpu": c.value})
		if len(r.Warnings) != c.warningCt {
			t.Errorf("gpu=%q: warnings=%d want %d (%v)", c.value, len(r.Warnings), c.warningCt, r.Warnings)
		}
	}
}

func TestValidateUniverseLabels_ReservedTier(t *testing.T) {
	cases := []struct {
		value     string
		warningCt int
	}{
		{"low", 0},
		{"medium", 0},
		{"high-performance", 0},
		{"ultra", 1},
	}
	for _, c := range cases {
		r := ValidateUniverseLabels(map[string]string{"tier": c.value})
		if len(r.Warnings) != c.warningCt {
			t.Errorf("tier=%q: warnings=%d want %d (%v)", c.value, len(r.Warnings), c.warningCt, r.Warnings)
		}
	}
}

func TestValidateUniverseLabels_ReservedSecurityLevel(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"security_level": "trusted"})
	if !r.OK || len(r.Warnings) != 0 {
		t.Errorf("security_level=trusted should be OK: %+v", r)
	}
	r = ValidateUniverseLabels(map[string]string{"security_level": "invalid"})
	if !r.OK || len(r.Warnings) != 1 {
		t.Errorf("security_level=invalid should warn: %+v", r)
	}
	if !strings.Contains(r.Warnings[0], "not in allowed values") {
		t.Errorf("warning should mention 'not in allowed values': %q", r.Warnings[0])
	}
}

func TestValidateUniverseLabels_ReservedLoad(t *testing.T) {
	for _, v := range []string{"idle", "low", "medium", "high", "overloaded"} {
		r := ValidateUniverseLabels(map[string]string{"load": v})
		if !r.OK || len(r.Warnings) != 0 {
			t.Errorf("load=%q should be OK: %+v", v, r)
		}
	}
}

func TestValidateUniverseLabels_ReservedUniverseRole(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"universe_role": "compute-pool"})
	if !r.OK || len(r.Warnings) != 0 {
		t.Errorf("universe_role=compute-pool should be OK: %+v", r)
	}
	r = ValidateUniverseLabels(map[string]string{"universe_role": "invalid"})
	if len(r.Warnings) != 1 {
		t.Errorf("universe_role=invalid should warn: %+v", r)
	}
}

// =============================================================================
// 自由 labels 命名规范
// =============================================================================

func TestValidateUniverseLabels_FreeLabelSnakeCase(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"department": "healthcare"})
	if !r.OK || len(r.Warnings) != 0 {
		t.Errorf("department=healthcare should be OK: %+v", r)
	}
}

func TestValidateUniverseLabels_FreeLabelKebabCase(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"cost-center": "eng-001"})
	if !r.OK || len(r.Warnings) != 1 {
		t.Errorf("cost-center should warn: %+v", r)
	}
	if !strings.Contains(r.Warnings[0], "cost_center") {
		t.Errorf("warning should suggest cost_center: %q", r.Warnings[0])
	}
}

func TestValidateUniverseLabels_FreeLabelCamelCase(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"myLabel": "value"})
	if len(r.Warnings) != 1 {
		t.Errorf("myLabel should warn: %+v", r)
	}
	if !strings.Contains(r.Warnings[0], "my_label") {
		t.Errorf("warning should suggest my_label: %q", r.Warnings[0])
	}
}

func TestValidateUniverseLabels_FreeLabelEmptyValue(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{"department": ""})
	if len(r.Warnings) != 1 {
		t.Errorf("department=empty should warn: %+v", r)
	}
	if !strings.Contains(r.Warnings[0], "empty value") {
		t.Errorf("warning should mention empty value: %q", r.Warnings[0])
	}
}

// =============================================================================
// 多 labels 组合
// =============================================================================

func TestValidateUniverseLabels_MultipleReserved(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{
		"region": "cn-shanghai",
		"gpu":    "true",
		"tier":   "high-performance",
		"load":   "idle",
	})
	if !r.OK || len(r.Warnings) != 0 {
		t.Errorf("4 reserved labels should be OK: %+v", r)
	}
}

func TestValidateUniverseLabels_MixedWarnings(t *testing.T) {
	r := ValidateUniverseLabels(map[string]string{
		"region":        "cn-shanghai",
		"tier":          "ultra",     // warning
		"department":    "rnd",       // OK
		"non-standard":  "x",         // warning
		"myCustomLabel": "y",         // warning
	})
	if !r.OK {
		t.Errorf("mixed warnings should be OK=true: %+v", r)
	}
	if len(r.Warnings) != 3 {
		t.Errorf("expected 3 warnings, got %d (%v)", len(r.Warnings), r.Warnings)
	}
}

// =============================================================================
// 白名单常量完整性
// =============================================================================

func TestReservedLabelKeys_6Keys(t *testing.T) {
	expected := []string{"region", "gpu", "tier", "security_level", "load", "universe_role"}
	if len(ReservedLabelKeys) != 6 {
		t.Errorf("ReservedLabelKeys should have 6 keys, got %d: %v", len(ReservedLabelKeys), ReservedLabelKeys)
	}
	for _, e := range expected {
		found := false
		for _, k := range ReservedLabelKeys {
			if k == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ReservedLabelKeys should contain %q, got %v", e, ReservedLabelKeys)
		}
	}
}

func TestIsReservedLabelKey(t *testing.T) {
	if !IsReservedLabelKey("region") {
		t.Error("region should be reserved")
	}
	if !IsReservedLabelKey("tier") {
		t.Error("tier should be reserved")
	}
	if IsReservedLabelKey("department") {
		t.Error("department should NOT be reserved")
	}
}

// =============================================================================
// Agent + AgentRegisterRequest 结构体集成测试
// =============================================================================

func TestAgent_UniverseLabelsField(t *testing.T) {
	// 老 client 不传 → nil
	a := Agent{Name: "test"}
	if a.UniverseLabels != nil {
		t.Error("uninitialized UniverseLabels should be nil")
	}

	// 新 client 传 map
	a2 := Agent{
		Name:           "test2",
		UniverseLabels: map[string]string{"region": "cn-shanghai", "gpu": "true"},
	}
	if a2.UniverseLabels["region"] != "cn-shanghai" {
		t.Error("UniverseLabels should store region=cn-shanghai")
	}

	// JSON 序列化测试
	a3 := Agent{
		Name:           "test3",
		UniverseLabels: map[string]string{"region": "cn-shanghai"},
	}
	// 不在 test 测 JSON,只验证字段存在
	if a3.UniverseLabels == nil {
		t.Error("UniverseLabels should be set")
	}
}

func TestAgentRegisterRequest_UniverseLabelsField(t *testing.T) {
	req := AgentRegisterRequest{
		Name:           "agent1",
		URL:            "https://example.com",
		Universes:      []string{"universe-a"},
		UniverseLabels: map[string]string{"tier": "high-performance"},
	}
	if req.UniverseLabels["tier"] != "high-performance" {
		t.Error("AgentRegisterRequest should store UniverseLabels")
	}
}

// =============================================================================
// LogLabelsValidation 函数(无 log 捕获,只验证不 panic)
// =============================================================================

func TestLogLabelsValidation_NoLog(t *testing.T) {
	// 全 OK 不输出
	r := LabelsValidationResult{OK: true}
	LogLabelsValidation(r, "test") // 不 panic 即通过
}

func TestLogLabelsValidation_WithWarnings(t *testing.T) {
	r := LabelsValidationResult{
		OK:       true,
		Warnings: []string{"warn1", "warn2"},
	}
	LogLabelsValidation(r, "test")
}

func TestLogLabelsValidation_WithErrors(t *testing.T) {
	r := LabelsValidationResult{
		OK:     false,
		Errors: []string{"err1"},
	}
	LogLabelsValidation(r, "test")
}
