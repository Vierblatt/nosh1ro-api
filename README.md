# nosh1ro-api

Go 个人博客后端 API，为 [nosh1ro.top](https://nosh1ro.top) 提供服务。

## 技术栈

| 层 | 技术 |
|---|---|
| HTTP 框架 | [Gin](https://github.com/gin-gonic/gin) |
| 数据库 | MySQL 8.0 |
| 缓存 | [Redis](https://redis.io) — Cache-Aside 模式，ZSet 热门文章，滑动窗口限流 |
| 消息队列 | [RabbitMQ](https://rabbitmq.com) — Topic Exchange + DLX 死信队列，事件驱动 |
| 搜索引擎 | [Elasticsearch 8](https://elastic.co) — IK 中文分词，高亮，聚合（可选） |
| 认证 | JWT（HS256，72h 过期）+ bcrypt |
| Markdown | [goldmark](https://github.com/yuin/goldmark) + GFM + Chroma 代码高亮 |
| 加密 | AES-256-GCM + PBKDF2（100k 迭代） |
| 日志 | `log/slog`（JSON 格式） |
| 容器 | Docker Compose 一键编排 MySQL + Redis + RabbitMQ |

### 架构亮点

- **缓存策略**：Cache-Aside 模式，文章列表/详情自动缓存（TTL + 主动失效），Redis 不可用时自动降级
- **事件驱动**：文章增删改 → RabbitMQ Topic Exchange → 异步同步 ES + 缓存失效，失败消息进入 DLX 死信队列
- **优雅降级**：Redis / RabbitMQ / ES 均为可选组件，不可用时自动 fallback 到内置实现
- **搜索引擎降级**：ES 不可用时自动切换 SQL LIKE 搜索，保证服务可用

## 项目结构

```
nosh1ro-api/
├── cmd/api/main.go                     # 入口：依赖装配 → 启动
├── internal/
│   ├── config/config.go                # 环境变量配置 + 校验
│   ├── model/model.go                  # 领域模型 + 类型安全常量
│   ├── store/store.go                  # 数据访问层
│   ├── cache/cache.go                  # Redis 缓存层（Cache-Aside + ZSet 热门）
│   ├── es/es.go                        # Elasticsearch 客户端（IK 分词 + 搜索）
│   ├── events/events.go                # RabbitMQ 事件总线（DLX 死信队列）
│   ├── handler/
│   │   ├── handler.go                  # Store 接口 + AppError 统一错误
│   │   ├── post.go                     # 公开 API（健康检查/文章/搜索/标签/验证）
│   │   ├── admin.go                    # 管理 API（登录/CRUD/设置/reindex）
│   │   ├── auth.go                     # 注册/验证/登录
│   │   ├── feed.go                     # RSS 2.0 Feed
│   │   └── handler_test.go             # HTTP 集成测试
│   ├── middleware/middleware.go         # CORS / JWT Auth / Rate Limit / Request ID / Slog Logger
│   ├── auth/auth.go + auth_test.go     # JWT + bcrypt
│   ├── markdown/markdown.go + test     # goldmark 渲染
│   ├── crypto/crypto.go + test         # AES-256-GCM 解密
│   └── seed/seed.go                    # 初始种子数据（6 篇）
├── docker-compose.yml                  # MySQL + Redis + RabbitMQ
├── docker/es.Dockerfile                # ES + IK 分词器
├── .github/workflows/ci.yml            # CI：test+vet+build
├── Dockerfile                          # 多阶段构建（scratch ~5MB）
└── Makefile                            # run / test / vet / build
```

**设计原则**：
- `internal/` 利用 Go 编译器强制包可见性，外部项目不可导入
- Handler 层面向接口编程，依赖注入 + 构造函数装配
- 遵循 "accept interfaces, return structs" Go 习惯
- 所有中间件可插拔，不可用时自动降级

## 快速开始

### 1. 启动基础设施

```bash
docker compose up -d   # MySQL + Redis + RabbitMQ
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env，修改 JWT_SECRET 和 ADMIN_PASSWORD
```

### 3. 运行

```bash
go run ./cmd/api/
# 或
make run
```

服务启动后访问 `http://localhost:8080/api/health`。

## API 端点

### 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/health` | 健康检查（含 DB + Redis + ES 状态） |
| `GET` | `/api/posts?page=&size=&tag=&category=&q=` | 文章列表（分页 + 筛选 + 搜索） |
| `POST` | `/api/posts/search` | 全文搜索（ES 分词 + 高亮 + 聚合，降级到 SQL） |
| `GET` | `/api/posts/:id` | 文章详情 |
| `POST` | `/api/posts/:id/verify` | 加密文章密码验证 |
| `GET` | `/api/tags` | 所有标签 |
| `GET` | `/api/feed.xml` | RSS 2.0 Feed |

### 认证接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/auth/register` | 用户注册 |
| `GET` | `/api/auth/verify?token=` | 邮箱验证 |
| `POST` | `/api/auth/resend-verification` | 重发验证邮件 |

### 管理接口（需 JWT）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/admin/login` | 登录 |
| `GET` | `/api/admin/posts` | 全部文章（含草稿） |
| `POST` | `/api/admin/posts` | 新建文章 |
| `PUT` | `/api/admin/posts/:id` | 更新文章 |
| `DELETE` | `/api/admin/posts/:id` | 删除文章 |
| `POST` | `/api/admin/reindex` | 重建 ES 索引 |
| `GET` | `/api/admin/settings` | 博客设置 |
| `PUT` | `/api/admin/settings` | 更新设置 |

### 错误响应格式

```json
{
  "code": "NOT_FOUND",
  "message": "resource not found"
}
```

预定义错误码：`NOT_FOUND` / `UNAUTHORIZED` / `FORBIDDEN` / `BAD_REQUEST` / `INTERNAL` / `RATE_LIMITED`

## Docker

```bash
# 全部服务
docker compose up -d

# 仅 API
docker build -t nosh1ro-api .
docker run -p 8080:8080 \
  -e DB_DSN="root:password@tcp(host.docker.internal:3306)/blog?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true" \
  -e JWT_SECRET=dev-secret \
  -e ADMIN_PASSWORD=test123 \
  nosh1ro-api
```

## 测试

```bash
make test    # go test -race -count=1 ./...
make vet     # go vet ./...
make build   # CGO_ENABLED=0 go build
```

## 数据库设计

### posts
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(255) PK | 文章 slug |
| title | TEXT | 标题 |
| content | TEXT | Markdown 原文 |
| content_html | TEXT | 渲染后 HTML |
| summary | TEXT | 摘要 |
| date | TEXT | 发布日期 |
| category | VARCHAR(255) | 分类 |
| status | VARCHAR(50) | draft / published |
| encrypted | TINYINT(1) | 是否加密 |
| enc_salt/nonce/cipher | TEXT | AES-256-GCM 加密数据 |

### post_tags
| 字段 | 类型 | 说明 |
|------|------|------|
| post_id | VARCHAR(255) FK | 关联 posts.id (CASCADE) |
| tag | VARCHAR(255) FK | 关联 tags.name (CASCADE) |

加密文章：`content` 为空，加密数据存 `enc_salt/nonce/cipher` 字段。客户端 `POST /api/posts/:id/verify` 传入密码，服务端 AES-256-GCM 解密返回明文。

## 安全措施

- ✅ 全参数化 SQL 查询（防注入）
- ✅ JWT + bcrypt 管理认证（JWT 72h 过期）+ 角色权限隔离
- ✅ Redis 滑动窗口限流（降级到内存令牌桶）
- ✅ CORS 限制、统一 AppError 类型
- ✅ 优雅关闭（SIGINT/SIGTERM，5s 超时）
- ✅ Read/Write/Idle 超时配置
- ✅ `-race` 竞态检测通过

## CI

GitHub Actions：每次 push/PR 运行 `go test -race` + `go vet` + `go build`。
