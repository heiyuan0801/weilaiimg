# 贡献指南

感谢你参与 WeiLai ImageHub。

## 开发环境

- Node.js 22
- pnpm
- Go 1.23
- Docker Engine 与 Docker Compose v2

```bash
git clone https://github.com/heiyuan0801/weilaiimg.git
cd weilaiimg
pnpm install --frozen-lockfile
cp .env.example .env
```

前端使用 `pnpm dev`，后端进入 `backend` 后执行 `go run ./cmd/server`。也可以执行 `docker compose up -d --build` 启动完整环境。

## 提交改动

1. 从 `main` 创建功能或修复分支。
2. 不要提交 `.env`、密码、Token、私钥和生产数据。
3. 保持改动范围清晰，并同步相关文档。
4. 提交前运行：

```bash
pnpm build
cd backend && go test ./...
docker compose config --quiet
```

5. Pull Request 需要说明问题、最终行为和验证结果。

安全漏洞请按照 [SECURITY.md](../SECURITY.md) 私下报告，不要创建公开 Issue。
