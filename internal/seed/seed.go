package seed

import (
	"context"
	"time"

	"github.com/Vierblatt/nosh1ro-api/internal/crypto"
	"github.com/Vierblatt/nosh1ro-api/internal/markdown"
	"github.com/Vierblatt/nosh1ro-api/internal/model"
	"github.com/Vierblatt/nosh1ro-api/internal/store"
)

func Posts(store *store.Store) error {
	ctx := context.Background()
	count, err := store.CountPosts(ctx, model.PostFilter{})
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	goPlan, err := loadEncryptionFile("go-plan-encryption.json")
	if err != nil {
		return err
	}
	blogDeploy, err := loadEncryptionFile("encryption.json")
	if err != nil {
		return err
	}

	now := time.Now()
	type seedPost struct {
		post     model.Post
		markdown string
	}

	posts := []seedPost{
		{
			post: model.Post{ID: "fullstack-review", Title: "从零搭建零端口暴露的个人博客：全链路踩坑复盘", Date: "2026-06-04", Status: "published", Category: "技术",
				Tags: []string{"DevOps", "安全", "Cloudflare", "Nginx", "网络"},
			},
			markdown: fullstackReviewMD,
		},
		{
			post: model.Post{ID: "go-plan", Title: "Go 学习路线图", Date: "2026-06-03", Status: "published", Category: "编程",
				Tags: []string{"Go"}, Encrypted: true, Encryption: goPlan,
			},
		},
		{
			post: model.Post{ID: "cloudflare", Title: "上了 Cloudflare，顺便修了个 HTTPS 的坑", Date: "2026-06-03", Status: "published", Category: "技术",
				Tags: []string{"Cloudflare", "HTTPS", "前端"},
			},
			markdown: cloudflareMD,
		},
		{
			post: model.Post{ID: "blog-deploy", Title: "博客部署：从 DNS 到安全加固的完整链路", Date: "2026-06-02", Status: "published", Category: "DevOps",
				Tags: []string{"Nginx", "部署", "安全"}, Encrypted: true, Encryption: blogDeploy,
			},
		},
		{
			post: model.Post{ID: "domain-up", Title: "域名上线", Date: "2026-06-02", Status: "published", Category: "杂谈",
				Tags: []string{"域名"},
			},
			markdown: domainUpMD,
		},
		{
			post: model.Post{ID: "frp-tunnel", Title: "路由器 FRP 穿透", Date: "2026-06-01", Status: "published", Category: "网络",
				Tags: []string{"FRP", "路由器", "网络"},
			},
			markdown: frpTunnelMD,
		},
	}

	for _, sp := range posts {
		p := sp.post
		md := sp.markdown
		p.Content = md
		p.ContentHTML = markdown.Render(md)
		p.Summary = markdown.ExtractSummary(p.ContentHTML, 200)
		p.CreatedAt = now
		p.UpdatedAt = now
		if err := store.InsertPost(ctx, &p); err != nil {
			return err
		}
	}
	return nil
}

func loadEncryptionFile(path string) (*model.EncryptionData, error) {
	salt, err := crypto.LoadEncryptionJSONField(path, "salt")
	if err != nil {
		return nil, err
	}
	nonce, err := crypto.LoadEncryptionJSONField(path, "nonce")
	if err != nil {
		return nil, err
	}
	ciphertext, err := crypto.LoadEncryptionJSONField(path, "ciphertext")
	if err != nil {
		return nil, err
	}
	return &model.EncryptionData{Salt: salt, Nonce: nonce, Ciphertext: ciphertext}, nil
}

const fullstackReviewMD = `把 nosh1ro.top 从"公网端口全开"改成了"零入站端口"，整个过程踩了不少坑，顺便把整个项目的技术栈从头复盘一遍。

## 一、整体架构

` + "```" + `
用户 HTTPS
  │
Cloudflare Edge (CDN / WAF / SSL)
  │ QUIC Tunnel（出站长连接，非入站端口）
  │
cloudflared (systemd 守护)
  │ http://127.0.0.1:80
  │
Nginx → /var/www/blog （Vue 3 + Vite 静态站点）
` + "```" + `

**关键决策**：服务器不监听任何公网 HTTP/HTTPS 端口，所有流量通过 Cloudflare Tunnel 反向通道进来。

## 二、域名 & 备案绕坑

国内 ECS 绑域名没备案，运营商会直接劫持拦截。接入 Cloudflare 橙色云朵后，访客 DNS 解析到 CF 的海外边缘节点 IP，**流量不经过国内域名白名单校验链路**，绕过了备案拦截。

## 三、HTTPS 与 Web Crypto 踩坑

` + "`window.crypto.subtle`" + ` 只在**安全上下文**（HTTPS 或 localhost）下可用。不是在 HTTP 环境调试加密文章功能，` + "`crypto.subtle`" + ` 一直是 ` + "`undefined`" + `，` + "`importKey`" + ` 调用直接报 ` + "`Cannot read properties of undefined`" + `。

排查过程：
- 自签证书 → HTTPS 通了但浏览器弹警告，生产不可用
- Cloudflare Universal SSL（Google Trust Services 签发）→ 全浏览器信任，API 恢复正常

这不是代码的 bug，是浏览器安全策略在 HTTP 下根本不给你 API。

## 四、Nginx 安全加固

**资源防护**
- ` + "`try_files $uri $uri/ =404`" + ` — 杜绝目录浏览
- ` + "`location ~ /\\.`" + ` — 拦截 ` + "`.env`" + `、` + "`.git`" + ` 等隐藏文件泄露
- ` + "`server_tokens off`" + ` — 隐藏 Nginx 版本号

**速率限制**
` + "```nginx" + `
limit_req_zone $binary_remote_addr zone=mylimit:10m rate=10r/s;
limit_req zone=mylimit burst=20 nodelay;
` + "```" + `

**最小权限**：Nginx worker 跑在 ` + "`www-data`" + ` 用户。

## 五、Cloudflare Tunnel 实战

核心思路：**服务器主动出站连 CF，公网不留任何 HTTP 入站口。**

踩坑：
- cloudflared 无头服务器认证需要复制 URL 到本地浏览器完成 OAuth
- systemd ` + "`Type=notify`" + ` 兼容性问题，换成 ` + "`Type=simple`" + `
- DNS 从 A 记录切到 ` + "`*.cfargotunnel.com`" + ` CNAME

## 六、学习价值

从 DNS 解析、VPC 网络、云防火墙、Nginx 深度配置、TLS 安全规范、浏览器安全策略、CDN 原理、内网穿透到 systemd 服务管理——全链路实操覆盖了后端运维 + 前端浏览器底层规范。`

const cloudflareMD = `把 nosh1ro.top 接入了 Cloudflare。免费的 CDN + HTTPS + DDoS 防护。

### 为什么搞 Cloudflare

两个原因：一是国内 ISP 没备案会拦截域名，CF 的 edge 节点在海外，访问者连的是 CF 的 IP，不走国内运营商检测。二是自动 HTTPS——CF 边缘签发 Google Trust Services 证书。

### 意外收获：修了 crypto.subtle 的 bug

之前那篇加密文章，浏览器一直报 ` + "`Cannot read properties of undefined (reading 'importKey')`" + `。排查了半天发现不是密码问题——浏览器的 Web Crypto API 只能在**安全上下文**里用，也就是 HTTPS 或 localhost。

之前 HTTP 访问，` + "`crypto.subtle`" + ` 直接是 ` + "`undefined`" + `。自签名证书虽然 HTTPS 通了但弹警告。现在 CF 一发搞定。

### 教训

Web Crypto API 和 Service Worker、Geolocation 等现代浏览器 API 都要求安全上下文。`

const domainUpMD = `买了 nosh1ro.top，配了 DNS 指向华为云服务器。顺便把短链接项目也部署上去了。

踩坑记录：MySQL 暴露公网被扫、Docker BuildKit 不走代理拉不下镜像、容器网络配错服务互不通……跑起来就行。`

const frpTunnelMD = `K2P Padavan 固件，frpc 连到云服务器 frps。现在可以从外网访问路由器管理和 SSH 了。

还被全网扫描器撞了一下——公网 IP 上线不到一小时就有俄罗斯的 IP 来扫端口。学会了什么叫"公网无差别扫描"。`
