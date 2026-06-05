# nosh1ro-api

Go 个人博客后端 API，为 [nosh1ro.top](https://nosh1ro.top) 提供服务。

## 技术栈

| 层 | 技术 |
|---|---|
| HTTP 框架 | [Gin](https://github.com/gin-gonic/gin) |
| 数据库 | SQLite（[modernc.org/sqlite](https://modernc.org/sqlite)，纯 Go，零 CGO） |
| 认证 | JWT（HS256，72h 过期）+ bcrypt |
| Markdown | [goldmark](https://github.com/yuin/goldmark) + GFM + 代码语法高亮（Chroma） |
| 加密 | AES-256-GCM + PBKDF2（100000 迭代） |

## 架构

```
nosh1ro-api/
├── main.go            # 入口：初始化 → 路由注册 → 优雅关闭
├── config.go          # 环境变量配置
├── model.go           # Post / AdminUser / BlogSettings / EncryptionData
├── store.go           # SQLite 数据层（CRUD、分页、搜索、标签关联表）
├── auth.go            # JWT 签发校验 + bcrypt
├── markdown.go        # Markdown → HTML 渲染
├── crypto.go          # AES-GCM 解密
├── middleware.go       # CORS / JWT 认证 / 令牌桶速率限制
├── handler_post.go    # 公开 API
├── handler_admin.go   # 管理后台 API
├── handler_feed.go    # RSS 2.0 Feed
├── seed.go            # 初始种子数据
├── *_test.go          # 单元测试 + 集成测试
```

## API 端点

### 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/health` | 健康检查 |
| `GET` | `/api/posts?page=&size=&tag=&category=&q=` | 文章列表（分页 + 标签/分类筛选 + 全文搜索） |
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
| `PUT` | `/api/admin/posts/:id` | 更新文章 |
| `DELETE` | `/api/admin/posts/:id` | 删除文章 |
| `GET` | `/api/admin/settings` | 获取博客设置 |
| `PUT` | `/api/admin/settings` | 更新博客设置 |

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
export DB_PATH="/opt/blog-api/blog.db"         # 可选
export BLOG_TITLE="nosh1ro"                    # 可选
export PORT="8080"                             # 可选
```

### 本地运行

```bash
go build -o server .
export JWT_SECRET=dev-secret ADMIN_PASSWORD=test123
./server
```

### 测试

```bash
go test -v -count=1 ./...
```

## 生产部署

服务器：华为云 ECS（Ubuntu 24.04），通过 Cloudflare Tunnel 暴露，不开放公网端口。

```bash
# 交叉编译
GOOS=linux GOARCH=amd64 go build -o server .

# 上传
scp server encryption.json go-plan-encryption.json root@server:/opt/blog-api/

# 重启
ssh root@server "systemctl restart blog-api"
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
- ✅ JWT + bcrypt 管理认证
- ✅ 令牌桶速率限制（公开 20rps，管理 5rps）
- ✅ CORS 限制仅 `nosh1ro.top`
- ✅ 错误信息不泄露内部状态
- ✅ 优雅关闭（SIGINT/SIGTERM）
