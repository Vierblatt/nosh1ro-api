# nosh1ro-api

Go 个人博客后端 API，为 [nosh1ro.top](https://nosh1ro.top) 提供服务。

## 项目结构

```
nosh1ro-api/
├── cmd/api/main.go                     # 入口：依赖装配 → 启动
├── internal/
│   ├── config/config.go                # 环境变量配置 + 校验
│   ├── model/model.go                  # 领域模型 + 类型安全常量
│   ├── store/store.go                  # SQLite 数据访问层
│   ├── handler/
│   │   ├── handler.go                  # Store 接口 + AppError 统一错误
│   │   ├── post.go                     # 公开 API（健康检查/文章/标签/验证）
│   │   ├── admin.go                    # 管理 API（登录/CRUD/设置）
│   │   ├── feed.go                     # RSS 2.0 Feed
│   │   └── handler_test.go             # HTTP 集成测试
│   ├── middleware/middleware.go         # CORS / JWT Auth / Rate Limit / Request ID / Slog Logger
│   ├── auth/auth.go + auth_test.go     # JWT + bcrypt
│   ├── markdown/markdown.go + test     # goldmark 渲染
│   ├── crypto/crypto.go + test         # AES-256-GCM 解密
│   └── seed/seed.go                    # 初始种子数据（6 篇）
├── .github/workflows/ci.yml            # CI：test+vet+build
├── Dockerfile                          # 多阶段构建（scratch ~5MB）
├── Makefile                            # run / test / vet / build / deploy
└── go.mod / go.sum
```

**设计原则**：
- `internal/` 利用 Go 编译器强制包可见性，外部项目不可导入
- Handler 层定义 `PostStore` / `AdminStore` 接口，`store.Store` 隐式实现
- 遵循 "accept interfaces, return structs" Go 习惯
- 依赖注入：`cmd/api/main.go` 负责装配，各组件通过构造函数接收依赖

## 技术栈

| 层 | 技术 |
|---|---|
| HTTP 框架 | [Gin](https://github.com/gin-gonic/gin) |
| 数据库 | SQLite（[modernc.org/sqlite](https://modernc.org/sqlite)，纯 Go，零 CGO） |
| 认证 | JWT（HS256，72h 过期）+ bcrypt |
| Markdown | [goldmark](https://github.com/yuin/goldmark) + GFM + Chroma 代码高亮 |
| 加密 | AES-256-GCM + PBKDF2（100k 迭代） |
| 日志 | `log/slog`（JSON 格式，生产环境） |
| 容器 | Docker 多阶段构建（scratch） |

## API 端点

### 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/health` | 健康检查（含 DB ping） |
| `GET` | `/api/posts?page=&size=&tag=&category=&q=` | 文章列表（分页 + 筛选 + 搜索） |
| `GET` | `/api/posts/:id` | 文章详情（含 `content_html`） |
| `POST` | `/api/posts/:id/verify` | 加密文章密码验证 → 返回解密内容 |
| `GET` | `/api/tags` | 所有标签 |
| `GET` | `/api/feed.xml` | RSS 2.0 Feed |

### 管理接口（需 JWT）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/admin/login` | 登录，返回 token |
| `GET` | `/api/admin/posts` | 列出全部文章（含草稿） |
| `POST` | `/api/admin/posts` | 新建文章 |
| `PUT` | `/api/admin/posts/:id` | 更新文章（部分更新） |
| `DELETE` | `/api/admin/posts/:id` | 删除文章 |
| `GET` | `/api/admin/settings` | 获取博客设置 |
| `PUT` | `/api/admin/settings` | 更新博客设置 |

### 错误响应格式

```json
{
  "code": "NOT_FOUND",
  "message": "resource not found"
}
```

预定义错误码：`NOT_FOUND` / `UNAUTHORIZED` / `FORBIDDEN` / `BAD_REQUEST` / `INTERNAL` / `RATE_LIMITED`

### 分页响应格式

```json
{
  "posts": [...],
  "total": 42,
  "page": 1,
  "size": 10
}
```

## 快速开始

### 环境变量

```bash
export JWT_SECRET="your-jwt-secret-key"       # 必需
export ADMIN_PASSWORD="your-admin-password"    # 必需
export ADMIN_USERNAME="admin"                  # 可选，默认 admin
export DB_PATH="blog.db"                       # 可选
export BLOG_TITLE="nosh1ro"                    # 可选
export PORT="8080"                             # 可选
```

### 本地运行

```bash
# 直接运行
make run

# 或手动
go run ./cmd/api/
```

### Docker

```bash
docker build -t nosh1ro-api .
docker run -p 8080:8080 \
  -e JWT_SECRET=dev-secret \
  -e ADMIN_PASSWORD=test123 \
  nosh1ro-api
```

### 测试

```bash
# 全部测试 + 竞态检测
make test

# 代码检查
make vet

# 构建
make build
```

## 生产部署

服务器：华为云 ECS（Ubuntu 24.04），通过 Cloudflare Tunnel 暴露，不开放公网端口。

```bash
# 一键部署
make deploy
```

### systemd 配置

```ini
[Unit]
Description=nosh1ro.top Go API
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/blog-api
ExecStart=/opt/blog-api/server
Restart=always
RestartSec=3
Environment="JWT_SECRET=<secret>"
Environment="ADMIN_PASSWORD=<password>"
Environment="DB_PATH=/opt/blog-api/blog.db"
Environment="BLOG_TITLE=nosh1ro"

[Install]
WantedBy=multi-user.target
```

## 数据库设计

### posts
```
id | title | content | content_html | summary | date | category | status | encrypted | enc_salt | enc_nonce | enc_cipher | created_at | updated_at
```

### tags（标签字典）
```
name (PK)
```

### post_tags（文章-标签关联）
```
post_id (FK → posts.id, CASCADE) | tag (FK → tags.name, CASCADE)
PRIMARY KEY (post_id, tag)
```

加密文章：`content` 字段为空，加密数据存储在 `enc_salt` / `enc_nonce` / `enc_cipher` 字段中。客户端调用 `POST /api/posts/:id/verify` 传入密码，服务端 AES-256-GCM 解密返回明文。

## 安全措施

- ✅ 全参数化 SQL 查询（防注入）
- ✅ JWT + bcrypt 管理认证（JWT 72h 过期）
- ✅ 令牌桶速率限制（公开 20rps，管理 5rps）
- ✅ CORS 限制仅 `nosh1ro.top`
- ✅ 统一 AppError 类型，错误信息不泄露内部状态
- ✅ 优雅关闭（SIGINT/SIGTERM，5s 超时）
- ✅ Read/Write/Idle 超时配置
- ✅ `-race` 竞态检测通过

## CI

GitHub Actions：每次 push/PR 运行 `go test -race` + `go vet` + `go build`。
