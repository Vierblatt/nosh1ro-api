package main

import (
	"context"
	"time"
)

func seedPosts(store *Store) error {
	ctx := context.Background()
	count, err := store.countPosts(ctx, PostFilter{})
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now()
	posts := []Post{
		{
			ID:          "fullstack-review",
			Title:       "从零搭建零端口暴露的个人博客：全链路踩坑复盘",
			Date:        "2026-06-04",
			Status:      "published",
			Tags:        []string{"博客", "DevOps", "安全"},
			Category:    "技术",
			Encrypted:   false,
			Content:     "把 nosh1ro.top 从\"公网端口全开\"改成了\"零入站端口\"，整个过程踩了不少坑，顺便把整个项目的技术栈从头复盘一遍。\n\n## 一、整体架构\n\n```\n用户 HTTPS\n  │\nCloudflare Edge (CDN / WAF / SSL)\n  │ QUIC Tunnel（出站长连接，非入站端口）\n  │\ncloudflared (systemd 守护)\n  │ http://127.0.0.1:80\n  │\nNginx → /var/www/blog （Vue 3 + Vite 静态站点）\n```\n\n**关键决策**：服务器不监听任何公网 HTTP/HTTPS 端口，所有流量通过 Cloudflare Tunnel 反向通道进来。\n\n## 二、域名 & 备案绕坑\n\n国内 ECS 绑域名没备案，运营商会直接劫持拦截。接入 Cloudflare 橙色云朵后，访客 DNS 解析到 CF 的海外边缘节点 IP，**流量不经过国内域名白名单校验链路**，绕过了备案拦截。裸 IP 直连反而会被封，这个反直觉的现实是个人静态站最头疼的一关。\n\n## 三、HTTPS 与 Web Crypto 踩坑\n\n`window.crypto.subtle` 只在**安全上下文**（HTTPS 或 localhost）下可用。之前在 HTTP 环境调试加密文章功能，`crypto.subtle` 一直是 `undefined`，`importKey` 调用直接报 `Cannot read properties of undefined`。\n\n排查过程：\n- 自签证书 → HTTPS 通了但浏览器弹警告，生产不可用\n- Cloudflare Universal SSL（Google Trust Services 签发）→ 全浏览器信任，API 恢复正常\n\n这不是代码的 bug，是浏览器安全策略在 HTTP 下根本不给你 API。同类受限的还有 Service Worker、Geolocation、Clipboard 高级接口——全部强制安全上下文。\n\n## 四、Nginx 安全加固\n\n生产环境的 Nginx 不是改个 root 就完事的：\n\n**资源防护**\n- `try_files $uri $uri/ =404` — 杜绝目录浏览\n- `location ~ /\\.` — 拦截 `.env`、`.git` 等隐藏文件泄露\n- `server_tokens off` — 隐藏 Nginx 版本号，不给扫描器提供指纹\n\n**安全响应头**\n- `X-Frame-Options: DENY` — 防 iframe 点击劫持\n- `X-Content-Type-Options: nosniff` — 禁止 MIME 类型嗅探\n- `X-XSS-Protection: 1; mode=block` — 浏览器内置 XSS 防御\n\n**速率限制**\n```nginx\nlimit_req_zone $binary_remote_addr zone=mylimit:10m rate=10r/s;\nlimit_req zone=mylimit burst=20 nodelay;\nlimit_req_status 429;\n```\n基于真实 IP 的令牌桶算法，单 IP 每秒 10 请求，突发 20，超限直接 429。在公网扫描器 7×24 遍历的环境下，这个配置必不可少。\n\n**最小权限**：Nginx worker 跑在 `www-data` 用户，只有 master 是 root。\n\n## 五、云安全组：虚拟化层拦截\n\n华为云安全组在宿主机虚拟化层就丢包，数据包**没到系统网卡就被丢弃了**，优先级高于 iptables。之前踩过一个坑：本机配了全局 `https_proxy=127.0.0.1:7890`，用 curl 调华为云 API 被代理劫持导致 SSL 握手失败，加 `--noproxy` 绕过才通。\n\n## 六、家庭网络基建：Padavan + FRP\n\nK2P 路由器刷 Padavan，跑 frpc 连到云服务器 frps，实现外网管理路由器和 SSH 内网设备。上线一小时就遭遇境外 IP 全端口扫描——直观感受了一把公网有多\"热闹\"，这也是后来下决心改用 Tunnel、关闭源站端口的直接原因。\n\n## 七、Cloudflare Tunnel 实战\n\n这是最近一次架构升级。核心思路：**服务器主动出站连 CF，公网不留任何 HTTP 入站口。**\n\n踩坑记录：\n- cloudflared 在无头服务器上认证，`tunnel login` 需要浏览器打开 URL，但服务器没有 GUI——需要复制 URL 到本地浏览器完成 OAuth\n- ARGO TUNNEL TOKEN（PEM 格式）和原生 base64 token 是两种格式，用错直接 \"token is not valid\"\n- systemd `Type=notify` 和 cloudflared 的兼容性问题，换成 `Type=simple` 才正常启动\n- DNS 从 A 记录切到 `*.cfargotunnel.com` CNAME，要先删旧记录再建新的，不能直接 PATCH\n\n部署完成后验证：\n```bash\n# 公网端口检测\ntimeout 3 bash -c 'echo >/dev/tcp/139.159.232.200/80'  # 超时 ✅\ntimeout 3 bash -c 'echo >/dev/tcp/139.159.232.200/443' # 超时 ✅\n```\n两个端口都关了，网站通过 CF Tunnel 正常运行。\n\n## 八、架构优缺点\n\n**优点**\n- 零入站端口：彻底杜绝端口扫描，攻击者连服务器在哪都不知道\n- 零成本：Cloudflare 免费 CDN、WAF、DDoS 防护\n- 部署轻量：纯静态前端，无后端依赖\n\n**代价**\n- 依赖 cloudflared 进程，断连站点就挂（systemd Restart=always 兜底）\n- 无法灰云直连源站调试，本地测试只能 `curl -H \"Host: nosh1ro.top\" http://127.0.0.1/`\n- 出站带宽受 ECS 实例限制（静态站点基本无感）\n\n## 九、后续路线\n\n1. **Go 后端接入**：Nginx 预留 `/api/*` 反向代理，Gin 项目监听 `127.0.0.1:8080`，前端不动\n2. **加密文章升级**：密钥校验从纯前端 Web Crypto 迁到 Go 后端\n3. **监控与告警**：Tunnel 健康检查 + 证书到期提醒\n\n## 十、学习价值\n\n从 DNS 解析、VPC 网络、云防火墙、Nginx 深度配置、TLS 安全规范、浏览器安全策略、CDN 原理、内网穿透到 systemd 服务管理——**全链路实操覆盖了后端运维 + 前端浏览器底层规范**，比只看书扎实得多。",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:        "go-plan",
			Title:     "Go 学习路线图",
			Date:      "2026-06-03",
			Status:    "published",
			Tags:      []string{"Go", "学习"},
			Category:  "编程",
			Encrypted: true,
			Encryption: &EncryptionData{
				Salt:       loadEncryptionJSONField("go-plan-encryption.json", "salt"),
				Nonce:      loadEncryptionJSONField("go-plan-encryption.json", "nonce"),
				Ciphertext: loadEncryptionJSONField("go-plan-encryption.json", "ciphertext"),
			},
			Content:   "",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:    "cloudflare",
			Title: "上了 Cloudflare，顺便修了个 HTTPS 的坑",
			Date:  "2026-06-03",
			Status: "published",
			Tags:   []string{"Cloudflare", "HTTPS", "前端"},
			Category: "技术",
			Content: "把 nosh1ro.top 接入了 Cloudflare。免费的 CDN + HTTPS + DDoS 防护，对个人博客来说简直白嫖神车。\n\n### 为什么搞 Cloudflare\n\n两个原因：一是国内 ISP 没备案会拦截域名，CF 的 edge 节点在海外，访问者连的是 CF 的 IP，不走国内运营商检测，自然不拦。二是自动 HTTPS——CF 边缘签发 Google Trust Services 证书，浏览器看到的是正经证书。\n\n### 意外收获：修了 crypto.subtle 的 bug\n\n之前那篇加密文章，浏览器一直报 `Cannot read properties of undefined (reading 'importKey')`。排查了半天发现不是密码问题——浏览器的 Web Crypto API 有个硬性要求：`crypto.subtle` 只能在**安全上下文**里用，也就是 HTTPS 或 localhost。\n\n之前 HTTP 访问，`crypto.subtle` 直接是 `undefined`。自签名证书虽然 HTTPS 通了但弹警告。现在 CF 一发搞定。\n\n### 教训\n\nWeb Crypto API 和 Service Worker、Geolocation 等现代浏览器 API 都要求安全上下文。不是代码写错了，是浏览器不给你 API。",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "blog-deploy",
			Title:     "博客部署：从 DNS 到安全加固的完整链路",
			Date:      "2026-06-02",
			Status:    "published",
			Tags:      []string{"博客", "部署", "Nginx"},
			Category:  "DevOps",
			Encrypted: true,
			Encryption: &EncryptionData{
				Salt:       loadEncryptionJSONField("encryption.json", "salt"),
				Nonce:      loadEncryptionJSONField("encryption.json", "nonce"),
				Ciphertext: loadEncryptionJSONField("encryption.json", "ciphertext"),
			},
			Content:   "",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:    "domain-up",
			Title: "域名上线",
			Date:  "2026-06-02",
			Status: "published",
			Tags:   []string{"域名", "部署"},
			Category: "杂谈",
			Content: "买了 nosh1ro.top，配了 DNS 指向华为云服务器。顺便把短链接项目也部署上去了。\n\n踩坑记录：MySQL 暴露公网被扫、Docker BuildKit 不走代理拉不下镜像、容器网络配错服务互不通……跑起来就行。",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:    "frp-tunnel",
			Title: "路由器 FRP 穿透",
			Date:  "2026-06-01",
			Status: "published",
			Tags:   []string{"网络", "FRP", "路由器"},
			Category: "网络",
			Content: "K2P Padavan 固件，frpc 连到云服务器 frps。现在可以从外网访问路由器管理和 SSH 了。\n\n还被全网扫描器撞了一下——公网 IP 上线不到一小时就有俄罗斯的 IP 来扫端口。学会了什么叫\"公网无差别扫描\"。",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	for i := range posts {
		posts[i].ContentHTML = renderMarkdown(posts[i].Content)
		posts[i].Summary = extractSummary(posts[i].ContentHTML, 200)
		if err := store.insertPost(ctx, &posts[i]); err != nil {
			return err
		}
	}
	return nil
}
