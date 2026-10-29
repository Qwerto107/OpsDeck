# 开发指南

[文档中心](README.md) · [实现说明](implementation.md) · [技术选型](tech-stack.md)

## 环境

当前构建使用 Go 1.26.10、Node.js 22 和 PostgreSQL 18。前端 TypeScript 固定为 5.9.3，依赖版本由 `go.sum` 和 `web/package-lock.json` 锁定。数据库结构使用 golang-migrate，不使用 GORM AutoMigrate。

## 本地启动

以下命令使用 Linux shell。Windows PowerShell 使用 `$env:变量名='值'`。先准备专用 PostgreSQL 数据库。管理员名默认 admin，首次临时密码自动生成并仅输出到初始化日志一次。

```sh
export DATABASE_URL='postgres://opsdeck:replace-with-db-password@127.0.0.1:5432/opsdeck?sslmode=disable'
export MASTER_KEY_FILE='./secrets/master.key'
export LISTEN_ADDR='127.0.0.1:8080'

export INIT_USERNAME='admin'
go run ./cmd/opsdeck bootstrap
```

初始化仅在没有用户时创建管理员，首次登录必须修改临时密码。后续启动保留密码和主密钥，不会再次输出临时密码。访问地址和可信代理在系统设置中调整。

另开终端启动前端：

```sh
cd web
npm ci
npm run dev
```

Vite 将 `/api` 转发到 `127.0.0.1:8080`。浏览器使用 <http://localhost:5173>，访问地址设置可留空，或填写此前端地址；不要混用 `localhost` 和 `127.0.0.1`。

## 构建单文件程序

在仓库根目录执行：

```sh
npm --prefix web ci
npm --prefix web run build
node scripts/prepare-webui.mjs
go build -tags with_frontend -trimpath -o bin/opsdeck ./cmd/opsdeck
```

Windows 可将输出改为 `bin/opsdeck.exe`。`prepare-webui.mjs` 把前端产物复制到内嵌目录，`with_frontend` 启用内嵌页面。未设置构建标签的开发程序可通过 `WEB_DIR` 指向构建后的 `web/dist`。

## 验证

```sh
go test ./... -count=1
go vet ./...
npm --prefix web test
npm --prefix web run build
npm --prefix web run format:check
```

没有 `TEST_DATABASE_URL` 时，PostgreSQL 集成测试会明确跳过。使用专用测试数据库验证迁移、会话、权限及任务边界：

```sh
export TEST_DATABASE_URL='postgres://test-user:test-password@127.0.0.1:5432/opsdeck_test?sslmode=disable'
go test ./... -count=1
```

集成测试创建独立的 `opsdeck_test_*` schema，结束时只清理该 schema。数据库用户需要创建 schema 的权限。云接口字段和请求参数使用模拟响应验证，真实云账号、国际站、OAuth2 提供方及 Linux 部署仍需环境联调。

## 开发约束

关键逻辑使用中文注释，复用现有 Cloud 接口、任务、缓存和授权实现。新增资源按实际查询能力扩展，不引入未使用的通用插件框架。

数据库变更增加版本化 SQL。云凭证、主密钥、初始密码文件、访问令牌和本地数据库不得提交。完整规则见 [AGENTS.md](../AGENTS.md)。
