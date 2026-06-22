// Package wau v0.8.0 M3-2A — Universe Labels 校验
//
// 跟 WAU-core-kernel internal/registry/universe_labels.go 语义对齐
//
// 跟 afp-protocol 端 src/universe_labels.ts 语义对齐
//
// 关键约束(per v0.8.0 M3 B 计划决策 2 软警告):
//   - SDK 端只预校验(减少 round-trip),server 端是 source of truth
//   - 软警告不阻断,只 log.Warn
//   - 老 client 不传 labels → nil/空 map,无 warning
//   - 4 SDK 漂移风险:kernel 公开 ReservedLabelKeys 常量作 source of truth
//     (本文件直接复制,未来可改成代码生成)
package wau

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

// =============================================================================
// 6 个 reserved labels 白名单
// =============================================================================
//
// 跟 WAU-core-kernel internal/registry/universe_labels.go 一致
// 跟 afp-protocol src/universe_labels.ts 一致
// server 是 source of truth,SDK 端复制(漂移风险 M5 联调时校对)

var reservedLabelsAllValues = map[string]map[string]bool{
	"region":         {}, // 自由字符串
	"gpu":            {"true": true, "false": true},
	"tier":           {"low": true, "medium": true, "high-performance": true},
	"security_level": {"trusted": true, "untrusted": true},
	"load":           {"idle": true, "low": true, "medium": true, "high": true, "overloaded": true},
	"universe_role":  {"business": true, "compute-pool": true},
}

var reservedLabelKeys = map[string]bool{
	"region":         true,
	"gpu":            true,
	"tier":           true,
	"security_level": true,
	"load":           true,
	"universe_role":  true,
}

var snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ReservedLabelKeys 公开常量(供 caller 引用,避免各自维护漂移)
var ReservedLabelKeys = []string{
	"region",
	"gpu",
	"tier",
	"security_level",
	"load",
	"universe_role",
}

// IsReservedLabelKey 检查 key 是否在 reserved 白名单
func IsReservedLabelKey(key string) bool {
	return reservedLabelKeys[key]
}

// =============================================================================
// 校验结果类型
// =============================================================================

// LabelsValidationResult 校验结果(跟 kernel 端 + AFP 端字段 1:1)
type LabelsValidationResult struct {
	OK       bool
	Warnings []string
	Errors   []string
}

// =============================================================================
// 核心校验函数
// =============================================================================

// ValidateUniverseLabels 校验单个 labels map
//
// 永远不抛,返 LabelsValidationResult
// SDK 端调用方应检查 r.OK,Warnings 走 log.Warn,Errors 走 log.Error
func ValidateUniverseLabels(labels map[string]string) LabelsValidationResult {
	result := LabelsValidationResult{OK: true}

	if len(labels) == 0 {
		return result
	}

	for key, value := range labels {
		// 自由 label key 命名 warning
		if !reservedLabelKeys[key] && !snakeCaseRegex.MatchString(key) {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				`free label "%s" should be snake_case (e.g. "%s")`,
				key, toSnakeCaseSDK(key),
			))
		}

		// reserved label 校验
		if reservedLabelKeys[key] {
			allowed, hasEnum := reservedLabelsAllValues[key]
			if value == "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					`reserved label "%s" has empty value (consider removing or setting valid value)`,
					key,
				))
			} else if hasEnum && len(allowed) > 0 && !allowed[value] {
				allowedList := sortedKeysSDK(allowed)
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					`reserved label "%s"="%s" not in allowed values [%s]`,
					key, value, strings.Join(allowedList, ", "),
				))
			}
			continue
		}

		// 自由 label 空 value warning
		if value == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				`free label "%s" has empty value`,
				key,
			))
		}
	}

	return result
}

// LogLabelsValidation 把校验结果走 log(SDK 默认 logger)
//
//   - warnings → log.Println(前缀 [WAU SDK warn])
//   - errors → log.Println(前缀 [WAU SDK error])
//
// 调用方应在 RegisterCard / RegisterAgent / RegisterPeer 前调
func LogLabelsValidation(r LabelsValidationResult, context string) {
	if r.OK && len(r.Warnings) == 0 && len(r.Errors) == 0 {
		return
	}
	prefix := fmt.Sprintf("[WAU SDK %s]", context)
	for _, w := range r.Warnings {
		log.Printf("%s warn: %s", prefix, w)
	}
	for _, e := range r.Errors {
		log.Printf("%s error: %s", prefix, e)
	}
}

// =============================================================================
// 内部工具
// =============================================================================

// sortedKeysSDK 稳定 map key 排序(测试断言友好)
func sortedKeysSDK(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toSnakeCaseSDK camelCase / kebab-case → snake_case
func toSnakeCaseSDK(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32) // 转小写
		} else if r == '-' || r == ' ' {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
