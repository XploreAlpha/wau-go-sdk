# ADR-0004: 5 场景契约测试归属(独立仓 vs Go SDK 仓共生)

> **Status:** Accepted (2026-06-14)
> **Deciders:** Claude + youhaoxi
> **Context:** 5 场景契约测试的黄金 JSON 放哪?3 SDK 怎么共享?

## Context

3 SDK 必须保持"5 场景契约"字节级一致(plan §8 验收)。需要 1 份**唯一真相源**(黄金 JSON)供 3 SDK 测试用,避免漂移。

5 场景(clinical/France/pain/sales/rare-disease)在 wau-intent 仓已有 Python 实现做参考:[`wau-intent/e2e_test/test_submit_l4.py`](../../../../../wau-intent/e2e_test/test_submit_l4.py)。

**3 个候选方案:**

| 方案 | 优点 | 缺点 |
|---|---|---|
| A. 留在 wau-intent 仓 | 单一真相源,改一处全 SDK 同步 | 跨仓 PR,review 慢;wau-intent 仓变动频繁 |
| B. 独立 wau-contract-tests 仓 / 跟 Go SDK 共生 | 3 SDK 各自只读引用;权限清晰;改动集中 | 改 schema 要动 2 仓(SDK + contract) |
| C. 复制到各 SDK 仓 | 各自独立,CI 快 | 漂移风险;3 份重复维护 |

## Decision

**选 B 子选项:跟 Go SDK 仓共生**。

具体:
1. **黄金 JSON 位置**:`wau-go-sdk/tests/contract-golden/scenario_*.json`(5 个文件,2026-06-14 已建)
2. **Python SDK / TS SDK** 通过 git submodule 引用:
   ```bash
   git submodule add https://github.com/wau/wau-go-sdk.git vendor/wau-contract-tests
   # vendor/wau-contract-tests/tests/contract-golden/*.json
   ```
3. **Mock kernel 逻辑** 跟 SDK 实现解耦(不依赖具体 SDK 类型),只返 5 场景响应
4. **契约测试** 在每个 SDK 仓独立跑,共用同一份黄金 JSON
5. **wau-intent 仓 e2e**:**不**作为 SDK 契约测试真相源(wau-intent 仓 e2e 仍保留作真 kernel 验证,但 SDK 契约测试用 mock kernel + Go SDK 黄金 JSON)

**为什么不选独立仓(wau-contract-tests):**
- 多 1 仓维护成本;5 场景黄金 JSON 是 Go SDK 仓的内部资产,SDK 仓改 schema 时同步改
- 独立仓跟 wau-go-sdk 同步 push,反而增加协调成本

**为什么不复制到各 SDK 仓:**
- 3 份黄金 JSON 漂移风险,1 个月后场景数据可能不一致
- 改 1 处要 PR 3 仓,review 爆炸

## Consequences

**正面:**
- 黄金 JSON 单一真相源,Go SDK 仓是天然 owner(SDK 第一启动)
- Python/TS SDK 通过 submodule 引用,改 1 处全 SDK 同步
- Mock kernel 跟 SDK 仓共生,SDK 加新方法可同步加 mock

**负面:**
- Python/TS SDK 仓需加 git submodule 步骤(`git submodule update --init`)
- 黄金 JSON 改时,Go SDK PR 同时影响 Python/TS SDK(需 review 时同步)
- 5 场景扩到 10 场景时,3 SDK 都要更新 submodule ref(但只 1 行)

**Risk:**
- submodule 拉取失败 → CI 红
  - **缓解**:`git submodule update --init --recursive` 写在 CI 第一步
- 黄金 JSON 跟 wau-intent 仓 e2e 漂移(场景改了 SDK 不知道)
  - **缓解**:每发版前跑真 kernel 5 场景 e2e,跟 SDK 契约测试交叉验证

## Alternatives Reconsidered

- 选 A(留 wau-intent)被排除:wau-intent 仓改 1 处,SDK 仓要 review 跨仓 PR
- 选 C(复制)被排除:漂移风险,3 仓协调成本爆炸

## References

- plan: [`lexical-orbiting-nova.md` §8 + §9 决策 4](../../../../../../.claude/plans/lexical-orbiting-nova.md)
- 黄金 JSON 位置: [`/home/inamoto888/project/wau-go-sdk/tests/contract-golden/`](../../../../tests/contract-golden/)
- Mock kernel: [`/home/inamoto888/project/wau-go-sdk/tests/mock_server.go`](../../../../tests/mock_server.go)
- wau-intent 5 场景参考: [`/home/inamoto888/project/wau-intent/e2e_test/test_submit_l4.py`](../../../../../wau-intent/e2e_test/test_submit_l4.py)
