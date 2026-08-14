# Contributing to wau-go-sdk

Thank you for your interest in contributing to the official Go SDK of the WAU network. This guide covers everything you need to send a pull request that lands cleanly.

[English](#) | [中文贡献指南](#中文贡献指南)

## Code of Conduct

The repository does not currently ship a separate `CODE_OF_CONDUCT.md`. By participating you agree to keep the discussion focused, respectful, and on-topic — file an issue if you see behavior that violates that spirit.

## Development Setup

**Requirements**

- Go **1.25+** (the CI matrix builds on 1.23, 1.24, and 1.25; please match 1.25 locally to avoid drift)
- Git
- A copy of [`wau-circuit`](https://github.com/wau/wau-circuit) checked out as a sibling directory at `../wau-circuit` — the `go.mod` uses a `replace` directive that points there. **Do not commit a fix** for that `replace`; the publish pipeline re-vendors `wau-circuit` for tagged releases, so editing `go.mod` should be limited to dependency additions inside `require`.

**One-time checkout**

```bash
git clone https://github.com/wau/wau-go-sdk.git
cd wau-go-sdk
# Required for `go mod download` to satisfy `replace github.com/XploreAlpha/circuit => ../wau-circuit`
git clone https://github.com/wau/wau-circuit.git ../wau-circuit
```

**Build**

```bash
go mod download
go build ./...
```

## Running Tests

```bash
go test ./...
```

The CI run uses `go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...` and enforces a total coverage floor of **80%**. Please keep new code covered at that level or above — small pure-function files are usually enough to drag the total back up. Do not gate a test on external network calls; everything in the SDK should pass against `httptest`-backed mock servers in the same module.

## Linting

There is no project-specific `golangci-lint` configuration in this repository, so the lint baseline is whatever `go vet ./...` reports. Please run:

```bash
go vet ./...
gofmt -l .
```

Both commands must come back empty. CI also runs `gofmt -l .` and treats any diff as a failure.

## Pull Request Process

1. **Fork** the repository (the `wau-*` repos are single-branch until v1.0.0, so do all work on a fork branch).
2. Branch from `main` with a descriptive name: `feat/<short-topic>`, `fix/<short-topic>`, `docs/<short-topic>`, etc.
3. Keep commits small and self-contained. Write a short body when the *why* is non-obvious.
4. Run `go vet ./...`, `gofmt -l .`, and `go test -race ./...` locally before pushing.
5. Push your branch and open a pull request against `main`. The PR template is free-form; the key fields are: **what changed**, **why**, and **how it was tested**.
6. The CI matrix (Go 1.23 / 1.24 / 1.25 on Linux + macOS, plus a coverage gate on Ubuntu 1.25) must be green before merge.
7. **Tagging is the maintainer's job.** Do not run `git tag` from a PR — the version-sync window (see below) determines the tag.

## Commit Message Convention

This repository does not enforce Conventional Commits; free-form, imperative-mood subjects are fine. A common shape that reviewers find helpful:

```
<scope>: <imperative summary>

- bullet for what changed
- bullet for why it matters

Refs: GH-#, D## / ARCHITECTURE.md#section
```

## Version Sync Policy

`wau-go-sdk` ships in lockstep with the rest of the [WAU 19-repo architecture](https://github.com/wau): wau-core-kernel and the sister SDKs (Python, TypeScript, Rust, Java, …). When any of those repos releases a `v1.3.X` patch or minor, **this repository must release the matching `v1.3.X` in the same window**, so that downstream consumers can pin one canonical version across stacks.

Concretely:

- A new feature lands in `wau-python-sdk` or `wau-core-kernel` first; the cross-SDK decision record names the target version (e.g. `v1.3.5`).
- This repository opens a backport PR that brings the same feature over, with byte-equal JSON alignment per the cross-SDK contract.
- The maintainer cuts the Git tag (`v1.3.X`) once both the source and the Go port land; do not tag PRs from your fork.
- Breaking changes flow only through a MAJOR bump, and only after a `WauWorkflow`-style byte-equal verification across all five SDKs.

If your change has cross-SDK implications, leave a note in the PR description naming the other repositories it has to land in.

---

## 中文贡献指南

感谢你对 WAU 网络官方 Go SDK 的兴趣。本节是上方英文贡献指南的中文版,内容逐项对齐,如有不一致以英文版为准。

### 行为准则

本仓库目前没有单独的 `CODE_OF_CONDUCT.md`。参与讨论时,请保持专注、尊重、就事论事;如遇违反此精神的行为,请开 issue 反馈。

### 开发环境

**前置**

- Go **1.25+**(CI matrix 同时跑 1.23 / 1.24 / 1.25,本地请对齐 1.25 避免漂移)
- Git
- 一份 `wau-circuit` 拷贝,放在同级目录 `../wau-circuit`(`go.mod` 里有 `replace github.com/XploreAlpha/circuit => ../wau-circuit` 指向那里)。**不要在 PR 里改 `go.mod` 的 `replace`** —— 发布流水线会在打 tag 时重新拉取 vendored `wau-circuit`,`go.mod` 的修改应仅限于在 `require` 块中新增依赖。

**一次性拉取**

```bash
git clone https://github.com/wau/wau-go-sdk.git
cd wau-go-sdk
# go mod download 依赖 ../wau-circuit,所以需要把它拉下来
git clone https://github.com/wau/wau-circuit.git ../wau-circuit
```

**构建**

```bash
go mod download
go build ./...
```

### 运行测试

```bash
go test ./...
```

CI 实际跑的是 `go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...`,并强制 **80%** 总覆盖率。新代码请保持同等或更高覆盖率 —— 加几个针对纯函数的小测试即可拉回总数。所有测试都不要发外网,本仓库的测试全部使用 `httptest` 起的本地 mock server。

### Lint

本仓库没有专门的 `golangci-lint` 配置,lint 基线就是 `go vet ./...`。提交前请跑:

```bash
go vet ./...
gofmt -l .
```

两条命令都必须返回空。CI 同样会跑 `gofmt -l .`,任何 diff 都会判定为失败。

### Pull Request 流程

1. **Fork** 仓库(`wau-*` 仓库到 v1.0.0 前只有单分支,所有改动在 fork 分支上完成)。
2. 从 `main` 新建分支,命名格式 `feat/<主题>` / `fix/<主题>` / `docs/<主题>` 等。
3. commit 尽量小而独立;遇到 *why* 不直白时,补一段正文。
4. 提交前本地跑 `go vet ./...`、`gofmt -l .`、`go test -race ./...` 三件套。
5. push 分支并开 PR 指回 `main`。PR 模板随意,核心三个字段:**改了什么**、**为什么**、**怎么测的**。
6. CI 矩阵(Go 1.23 / 1.24 / 1.25,Linux + macOS,Ubuntu 1.25 加 80% 覆盖率门槛)全绿才能合。
7. **打 tag 是维护者的活**,不要在 PR 里执行 `git tag` —— tag 版本号取决于下文版本同步窗口。

### Commit Message 约定

本仓库不强制 Conventional Commits;自由格式、祈使语气的主语即可。审阅者普遍认可的常见结构:

```
<scope>: <祈使句摘要>

- 改了什么 bullet
- 为什么重要 bullet

Refs: GH-#, D## / ARCHITECTURE.md#section
```

### 版本同步策略

`wau-go-sdk` 与 [WAU 19 仓架构](https://github.com/wau) 中的其他仓库(wau-core-kernel 以及 Python / TypeScript / Rust / Java 等兄弟 SDK)走 lockstep 发版:任何兄弟仓库释放 `v1.3.X` 后,**本仓库必须在同一窗口释放与之对齐的 `v1.3.X`**,便于下游用同一版本号锁住整套栈。

具体流程:

- 新特性先在 `wau-python-sdk` 或 `wau-core-kernel` 落地;跨 SDK 决策记录会写明目标版本(例如 `v1.3.5`)。
- 本仓库开 backport PR 把相同特性同步过来,JSON 按跨 SDK 契约做到 byte-equal。
- 源仓库与 Go 端都合并后,维护者统一打 Git tag(`v1.3.X`);请不要在 fork 的 PR 中自行打 tag。
- breaking change 只走 MAJOR,且必须先经过 `WauWorkflow` 风格的五 SDK byte-equal 验证。

如果你的改动涉及跨 SDK,请在 PR 描述里点名所有需要同步落地的仓库。
