# wau-go-sdk 部署(发布)

wau-go-sdk 是 **Go module**,无独立部署。B 端开发者 `go get` 后 import。

## 发布流程

```bash
# 1. 打 tag(用户手动,per [[feedback-cli-cant-push-git]])
git tag v1.1.0
git push origin v1.1.0

# 2. Go module 自动 proxy(无需额外操作)
```

## 版本兼容性

- **v1.1.x** ↔ wau-llm-router v0.9.x(per [[project-v0-9-0-M3-§3.7-chat-sdk-4langs-2026-06-30]])
- **wire / proto 100% 兼容** v0.8.0(v1.0.x)SDK

## 配置

```go
// SDK 无全局配置,每个 bot 自己 New(Config{})
type Config struct {
    Token    string  // env 占位,不进代码
    TenantID string
    Address  string  // 默认 :18431(wau-channel webhook)
}
```

**所有 token 用 `$VAR` 占位**(per 双 feedback)

## 升级路径

- v1.1.0 → v1.0.x:
  - wire 100% 兼容
  - 仅 import path 不变
- v1.1.0 → v1.2.0(roadmap):
  - 增加 streaming bot helper
  - 多 tenant tier 隔离
