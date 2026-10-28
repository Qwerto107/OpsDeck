# OpsDeck

## 功能

公司内部云运维平台，首期接入腾讯云 CVM 和 Lighthouse。

- 管理员创建账号，支持密码登录和通用 OAuth2 身份关联。
- 用户、角色、功能权限与云账号、资源类型、地域、实例范围授权。
- 服务器搜索、筛选、多字段排序和个人列显隐和拖动顺序偏好。
- 云监控当前数据、采样时间、无数据及过期状态。
- 单台启动、正常关机、正常重启、强制关机和强制重启。
- PostgreSQL 持久化任务、结果跟踪、审计日志和保留策略。

界面依据提供的 HTML 设计实现，采用 Vue 3、TypeScript、Vite 和 Naive UI。后端采用 Go、Gin、GORM、PostgreSQL 和 golang-migrate。详细范围见 [需求说明](requirements.md) 和 [技术选型](docs/tech-stack.md)。

## 本地开发

开发验证使用 Go 1.26.10、Node.js 22.22.2 和 PostgreSQL 18。依赖由 `go.sum` 和 `web/package-lock.json` 锁定。TypeScript 固定为 5.9.3，与当前 vue-tsc 配合使用。

以下为 Linux shell 示例。Windows PowerShell 使用 `$env:变量名='值'` 设置环境变量。

```sh
export DATABASE_URL='postgres://opsdeck:你的数据库密码@127.0.0.1:5432/opsdeck?sslmode=disable'
export MASTER_KEY_FILE='./secrets/master.key'
export APP_URL='http://localhost:5173'
go run ./cmd/opsdeck migrate

export INIT_USERNAME='admin'
export INIT_PASSWORD_FILE='./secrets/admin-password'
# 自行准备密码文件，密码为 12 至 72 字节。
go run ./cmd/opsdeck init
```

`init` 仅在没有用户时创建管理员，并在没有加密数据时生成主密钥。重复初始化不会重置管理员或覆盖密钥。初始化后应安全移出初始密码文件。

启动后端，一个进程同时运行 API 和后台 Worker：

```sh
go run ./cmd/opsdeck all
```

省略命令时默认使用 `all`。需要分开运行时，仍可分别使用 `api` 和 `worker` 子命令；合并模式无需额外启动 Worker。

另开终端启动前端：

```sh
cd web
npm ci
npm run dev
```

Vite 将 `/api` 转发到 `127.0.0.1:8080`。`APP_URL` 必须与浏览器实际访问的源一致，不能混用 localhost 与 127.0.0.1。

## Linux 部署

外置 Nginx 不包含在 Compose 应用栈中。Compose 包含 PostgreSQL、迁移服务和应用服务；应用服务在同一进程中运行 API 与 Worker。初始化服务通过 setup profile 按需运行。

```sh
cp .env.example .env
install -d -m 700 -o 10001 -g 10001 secrets
```

编辑 `.env`，设置实际域名、管理员账号和数据库密码。数据库密码建议使用随机十六进制字符串，避免连接 URL 中的保留字符。自行创建 `secrets/admin-password`，设置所有者为 `10001:10001`，权限为 `600`。

```sh
docker compose build
docker compose up -d postgres
docker compose run --rm migrate
docker compose --profile setup run --rm init
# 初始化完成后移出初始密码文件。
docker compose up -d api
```

从旧版独立 Worker 容器切换时，先停止旧 `worker` 容器，再启动合并服务。不要同时保留旧 Worker 和合并服务，避免增加不需要的后台进程。

API 仅发布宿主机回环地址 `127.0.0.1:8080`。参考 [外置 Nginx 配置](deploy/nginx.conf.example)，替换域名和证书。示例统一反向代理到应用，前端构建文件包含在应用镜像中。

需要使用转发的客户端 IP 时，将 `TRUSTED_PROXIES` 设置为实际 Nginx 来源网段；默认不信任转发头。生产环境的 `APP_URL` 使用 HTTPS，应用据此设置 Secure Cookie。外部 PostgreSQL 连接应按环境配置 TLS，Compose 内部示例使用私有网络。

## 主密钥

主密钥为首次初始化生成的 32 字节文件，保存在 `MASTER_KEY_FILE` 指定的位置。Compose 初始化服务可写入 `secrets`，API 和 Worker 仅读取该目录。

云凭证和 OAuth2 Client Secret 使用 AES-256-GCM 加密后存入数据库。用户密码使用 bcrypt 哈希保存。Session 令牌仅以 SHA-256 摘要保存，Cookie 中包含随机令牌。

主密钥需与数据库分别备份、配套恢复。已有校验记录或加密数据时，缺少密钥会阻止重新生成；密钥不匹配也会拒绝启动。当前没有自动密钥轮换工具，不能直接替换密钥文件。

## 接入与使用

1. 登录后在云账号页面填写腾讯云凭证，地域无需手填。
2. Worker 分别发现 CVM 和 Lighthouse 的全部可用地域，并同步实例。后续同步会自动发现新增地域。
3. 配置角色的功能权限和资源范围，再将角色分配给用户。内置只读和运维角色初始没有资源范围。
4. 在系统设置添加 OAuth2 提供方，将自动生成的回调地址配置到身份平台。
5. 用户先通过已有平台账号登录，在账号安全页绑定第三方身份，再使用第三方登录。

云账号所需的接口权限、可复制的 JSON 策略、只读接入方式和实例范围限制，见 [腾讯云 CAM 接入策略](docs/tencent-cam.md)。策略不声明官方 CAM 表未列出的 Lighthouse 地域查询 action，该 API 的实际鉴权行为仍需云端验证。应用使用腾讯云官方 Go SDK 的通用请求客户端调用产品接口。地域发现失败时保留已有地域和资源，并显示同步错误；平台角色资源范围继续生效。

## 监控与操作行为

- 只读取云监控接口，不安装 Agent，也不通过 SSH 采集。云端没有的数据明确显示为不可用。
- 当前查询 CPU、内存、实例级磁盘空间和公网入站、出站速率；区分 CVM 的 Mbps 与 Lighthouse 的 MB/s。
- 实例级磁盘空间不能冒充挂载点明细。未获取明细时显示不可用，不推测挂载点，也不以磁盘 IO 替代空间使用率。
- 进入服务器列表或详情页、点击更新时触发云端同步。页面打开且可见时，按设置间隔触发更新，默认 60 秒；页面先展示数据库快照，同步结束后重新读取。
- Worker 不再自主定时创建资源同步任务。关闭或离开所有资源页面后停止页面定时触发；已入队的任务仍会完成，操作结果核对与到期日志清理继续运行。保存已启用的云账号和手动同步账号仍会触发一次同步。
- 同步按账号执行，同一账号正在排队或同步时复用已有任务。查询结果仍按用户资源范围过滤。任务串行执行，云端采样周期、接口延迟和排队影响实际新鲜度，超过 5 分钟的采样标记过期。
- 普通关机、重启显式使用 SOFT；强制操作使用 HARD，要求输入完整实例名称确认。
- 同一资源只能存在一个未完成操作，提交前再次核对用户授权和云端状态。
- 提交前先持久化状态。发生超时或进程崩溃后只查询结果，不重复提交；无法确认的重启持续显示结果待确认。
- Worker 通过行锁和租约领取任务，结果每 15 秒核对一次，超过 10 分钟未完成显示待确认。审计到期清理每小时执行。

## 验证

```sh
go test ./...
go vet ./...
cd web
npm run build
npm run format:check
```

没有 `TEST_DATABASE_URL` 时，PostgreSQL 集成测试会明确跳过。使用专门的测试数据库运行：

```sh
export TEST_DATABASE_URL='postgres://测试账号:测试密码@127.0.0.1:5432/opsdeck_test?sslmode=disable'
go test ./... -count=1
```

测试会创建独立的 `opsdeck_test_*` schema，并在结束后仅清理该 schema。测试用户需要创建 schema 的权限。测试覆盖会话、CSRF、审计、授权隔离、排序分页、操作去重、超时不重提和密钥保护。

产品不内置演示账号或模拟云数据。真实腾讯云、第三方身份平台和目标 Linux 的 Compose、Nginx 配置仍需环境联调。
