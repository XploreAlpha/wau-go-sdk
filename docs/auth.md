# 鉴权(HS256 + JWT Bearer)

wau-go-sdk 支持可选的 HS256 JWT Bearer 鉴权(跟 wau-a2a-gateway 保持一致)。

## 启用鉴权

```go
c, err := wau.New("http://localhost:18400",
    wau.WithAuth(wau.AuthConfig{
        AgentName:    "my-agent",                          // 标识当前 agent
        SharedSecret: []byte(os.Getenv("WAU_JWT_SECRET")), // HS256 密钥
        Role:         wau.RoleTrustedAgent,                 // RBAC
    }),
)
```

不调用 `WithAuth()` = 不带鉴权(默认行为)。

## RBAC 角色

| Role | 权限 |
|---|---|
| `RoleKernelCore` | 全部(内部 kernel 用) |
| `RoleTrustedAgent` | Schedule + read-only(普通 agent) |
| `RoleExternalAgent` (默认) | Submit only(外部 SDK 用户) |

## JWT 结构

每个 HTTP 请求自动签一个新 JWT:

```json
{
  "agent": "my-agent",       // AuthConfig.AgentName
  "role":  "trusted_agent",  // AuthConfig.Role
  "iat":   1718342400,       // 签发时间
  "exp":   1718342700,       // 5 分钟后过期
  "jti":   "uuid-v4-string"  // 防重放
}
```

Header:
```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhZ2VudCI6...
```

**安全参数**:
- **算法**: HS256(对称,密钥双方共享)
- **过期**: 5 分钟(短;每次请求新签,减小重放窗口)
- **jti**: UUID v4 防重放(可选:服务端可维护 jti 黑名单)

## 配置技巧

### 从环境变量读密钥(推荐)

```go
import "os"

secret := os.Getenv("WAU_JWT_SECRET")
if secret == "" {
    log.Fatal("WAU_JWT_SECRET 未设置")
}
c, _ := wau.New("http://localhost:18400", wau.WithAuth(wau.AuthConfig{
    AgentName:    "my-agent",
    SharedSecret: []byte(secret),
}))
```

### 多实例共享 secret

server 端用同一密钥解析:
```go
// server 端伪代码
token, err := jwt.Parse(parts[1], func(t *jwt.Token) (any, error) {
    return []byte(os.Getenv("WAU_JWT_SECRET")), nil
})
```

## 不启用鉴权

```go
// 默认:不签 JWT
c, _ := wau.New("http://localhost:18400")
```

`Authorization` header 不会被设置。

## 错误处理

- `ErrUnauthorized` (401): 密钥不对 / 过期 / jti 黑名单 → 检查 server 时间差 / 重新发 secret
- `ErrForbidden` (403): 角色不够 → 改 Role 或联系 server 提升权限

## 协议参考

JWT 实现用 [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt)(HS256 算法)。
Python/TS SDK 用 [PyJWT](https://pyjwt.readthedocs.io/) / [jsonwebtoken](https://www.npmjs.com/package/jsonwebtoken),行为完全对齐。
