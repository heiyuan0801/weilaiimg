# WeiLai ImageHub

[![CI](https://github.com/heiyuan0801/weilaiimg/actions/workflows/ci.yml/badge.svg)](https://github.com/heiyuan0801/weilaiimg/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/heiyuan0801/weilaiimg)](https://github.com/heiyuan0801/weilaiimg/releases)
[![Container](https://img.shields.io/badge/GHCR-weilaiimg-blue?logo=docker)](https://github.com/heiyuan0801/weilaiimg/pkgs/container/weilaiimg)
[![License](https://img.shields.io/github/license/heiyuan0801/weilaiimg)](LICENSE)

WeiLai ImageHub 是一个可自行部署的多用户图床与媒体管理系统。项目将 React 管理端和 Go API 打包进同一个容器，并使用 PostgreSQL 保存业务数据、Redis 保存会话与限流状态。

## 功能

- 图片、GIF、SVG、MP4、WebM 上传与远程 URL 导入
- 本地、Telegram、S3、MinIO、Cloudflare R2 多存储通道
- 用户上传时选择存储通道，管理员设置默认通道
- 多用户、团队、成员邀请、容量与上传策略
- 私有、公开和仅链接三种访问方式
- 图片缩略图、视频元数据与异步媒体任务
- 注册、邮箱验证、密码找回、SMTP 与 OIDC 登录
- 套餐、订阅、Stripe 回调和团队计费基础能力
- CDN 地址、Cloudflare 缓存刷新、自定义域名与 Caddy TLS
- 中英文界面与浏览器语言自动识别
- 管理员统计、用户管理、媒体管理和系统设置

## 快速部署

要求：Docker Engine 24+ 和 Docker Compose v2。

```bash
git clone https://github.com/heiyuan0801/weilaiimg.git
cd weilaiimg
cp .env.example .env
```

编辑 `.env`，至少修改以下内容：

```dotenv
POSTGRES_PASSWORD=replace-with-a-strong-password
REDIS_PASSWORD=replace-with-a-strong-password
APP_SECRET=replace-with-a-long-random-secret
BOOTSTRAP_ADMIN_EMAIL=admin@example.com
BOOTSTRAP_ADMIN_PASSWORD=replace-with-a-strong-password
PUBLIC_URL=http://localhost:8080
```

使用 GitHub Container Registry 中的发布镜像：

```bash
docker compose pull
docker compose up -d
curl http://localhost:8080/healthz
```

浏览器打开 `http://localhost:8080`，使用 `.env` 中的初始管理员账号登录。初始管理员只会在数据库没有用户时创建。

### 从源码构建

```bash
docker compose up -d --build
```

需要通过本机 `7897` 端口代理下载依赖时：

```bash
./scripts/start-with-proxy.sh
```

## Docker 镜像

每个 GitHub Release 都会发布 `linux/amd64` 和 `linux/arm64` 镜像：

```bash
docker pull ghcr.io/heiyuan0801/weilaiimg:latest
docker pull ghcr.io/heiyuan0801/weilaiimg:v0.1.0
```

单独运行应用容器时还需要可访问的 PostgreSQL 和 Redis：

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL='postgres://imagehub:password@postgres:5432/imagehub?sslmode=disable' \
  -e REDIS_URL='redis://:password@redis:6379/0' \
  -e APP_SECRET='replace-with-a-long-random-secret' \
  -v imagehub_uploads:/app/uploads \
  ghcr.io/heiyuan0801/weilaiimg:latest
```

## 存储通道

登录管理员后台，在“系统设置 → Storage & CDN”中添加存储通道：

- **Local**：文件保存在 `/app/uploads`，Compose 默认使用持久卷。
- **Telegram**：需要在环境变量中设置 `TELEGRAM_BOT_TOKEN`，每个通道可以配置不同的聊天或频道 ID。
- **S3 compatible**：支持 AWS S3、MinIO、Cloudflare R2 及兼容服务，可以为每个通道设置 Endpoint、Region、Bucket、密钥、对象前缀和 Path Style。

管理员可以启用多个通道并指定默认通道。登录用户在媒体库上传文件或导入远程 URL 时可以选择任一已启用通道。

## 开发

前端要求 Node.js 22 和 pnpm，后端要求 Go 1.23。

```bash
pnpm install --frozen-lockfile
pnpm dev
```

另一个终端运行后端：

```bash
cd backend
go run ./cmd/server
```

常用检查：

```bash
pnpm build
cd backend && go test ./...
docker compose config --quiet
```

## 配置与运维

- 完整部署说明：[DEPLOYMENT.md](DEPLOYMENT.md)
- 后端开发说明：[backend/README.md](backend/README.md)
- 环境变量示例：[.env.example](.env.example)
- 版本记录：[CHANGELOG.md](CHANGELOG.md)
- 贡献指南：[.github/CONTRIBUTING.md](.github/CONTRIBUTING.md)
- 安全报告：[SECURITY.md](SECURITY.md)

生产环境必须替换示例密码和 `APP_SECRET`，启用 HTTPS，并备份 PostgreSQL 与实际使用的对象存储。不要把 `.env`、Bot Token、S3 密钥、SMTP 密码或 OAuth 密钥提交到 Git。

## License

[MIT](LICENSE)
