# 部署指南

[文档中心](README.md) · [二进制部署](binary.md) · [使用指南](usage.md)

## 部署方式

推荐在 Linux 上使用 Docker Compose，外置 Nginx 提供 HTTPS。应用进程同时提供页面、API 和 Worker。镜像内嵌前端，启动时自动迁移数据库、首次创建管理员和主密钥。

提供两份独立配置，选择其中一份启动：

| 文件                    | 数据库                  | 启动命令                                        |
| ----------------------- | ----------------------- | ----------------------------------------------- |
| `compose.yaml`（默认）  | 连接外部 PostgreSQL     | `docker compose up -d`                          |
| `compose.postgres.yaml` | 包含 PostgreSQL 18 容器 | `docker compose -f compose.postgres.yaml up -d` |

两份配置均固定使用 `ghcr.io/qwerto107/opsdeck:latest`，主密钥目录固定绑定为 `./secrets`。在同一部署目录选择一种方式运行，后续命令保持相同的 Compose 文件参数。

这套自动初始化流程适用于 `v0.1.4` 起的镜像。镜像需等待版本工作流完成后才可拉取；旧版本使用对应标签的部署文档，也可按“从源码构建”运行。

## 默认：连接外部 PostgreSQL

下载部署文件：

```sh
mkdir opsdeck
cd opsdeck
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/compose.yaml -o compose.yaml
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/.env.example -o .env
chmod 600 .env
```

先在 PostgreSQL 中创建空数据库和专用账号，在 `.env` 中填写 `DATABASE_URL`。应用需要建表及执行迁移的权限。连接地址必须能从容器访问，`127.0.0.1` 指向应用容器自身，外部数据库应使用实际主机地址。远程连接按环境配置 TLS。

```sh
docker compose up -d
docker compose logs api
```

本机打开 <http://127.0.0.1:8080>。管理员名默认 `admin`，`INIT_USERNAME` 留空或省略均使用该值。首次临时密码自动生成，在初始化事务提交成功后输出到日志，字段为 `temporary_password`，只输出一次。首次登录必须修改密码，已有用户时不生成或输出新密码。

## 同时运行 PostgreSQL 容器

改为下载 `compose.postgres.yaml`，无需默认的 `compose.yaml`：

```sh
mkdir opsdeck
cd opsdeck
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/compose.postgres.yaml -o compose.postgres.yaml
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/.env.example -o .env
chmod 600 .env
```

在 `.env` 中填写 `POSTGRES_PASSWORD`，建议使用随机十六进制字符串，避免连接 URL 的保留字符。此配置不使用 `DATABASE_URL`，数据库名和账号均为 `opsdeck`。

```sh
docker compose -f compose.postgres.yaml up -d
docker compose -f compose.postgres.yaml logs api
```

PostgreSQL 18 使用 `postgres-data` 卷，路径为 `/var/lib/postgresql`。不要直接替换数据库大版本镜像并复用原数据卷。

## 自动初始化与持久化

应用使用 `bootstrap` 命令。数据库就绪后执行内嵌的版本化迁移，随后校验密钥；没有用户时才创建管理员。初始化失败时 API 和 Worker 不会启动，原因写入错误日志。并发首次启动只初始化一次。

主密钥固定映射到部署目录的 `./secrets/master.key`。容器入口先准备绑定目录权限，再以 UID/GID `10001:10001` 运行应用，根文件系统只读。无需手动创建目录或执行 `chown`。已有数据库缺少原密钥或密钥不匹配时，拒绝启动，不会生成替代密钥。

旧部署已使用 `./secrets/master.key` 时，保留原目录、文件和 Compose 项目名 `opsdeck` 即可。上一版未发布的配置若使用 `secrets-data` 卷，先把原 `master.key` 安全复制到 `./secrets/master.key`，再切换配置。

## 从源码构建

```sh
git clone https://github.com/Qwerto107/OpsDeck.git
cd OpsDeck
cp .env.example .env
chmod 600 .env
```

选择数据库方式并填写对应变量。外部 PostgreSQL 使用：

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

包含 PostgreSQL 容器使用：

```sh
docker compose -f compose.postgres.yaml -f compose.build.yaml up -d --build
```

后续日志、停止和重建命令保留相同的 `-f` 参数。两种方式使用相同的自动初始化流程，无需单独执行 `migrate` 或 `init`，也无需另开 Worker。

## 部署变量

| 变量                | 用途                                                               |
| ------------------- | ------------------------------------------------------------------ |
| `DATABASE_URL`      | 默认 Compose 必填，外部 PostgreSQL 连接 URL                        |
| `POSTGRES_PASSWORD` | 仅带 PostgreSQL 的 Compose 使用                                    |
| `INIT_USERNAME`     | 可选，首次管理员名，默认 `admin`                                   |
| `MASTER_KEY_FILE`   | 二进制主密钥路径；Compose 固定为 `/run/opsdeck-secrets/master.key` |
| `LISTEN_ADDR`       | 二进制默认 `127.0.0.1:8080`；容器内为 `0.0.0.0:8080`               |
| `WEB_DIR`           | 覆盖内嵌前端的目录，正常发布部署无需设置                           |

原生程序不自动读取 `.env`，需由 shell 或服务管理器注入。访问地址、可信代理、会话时长、监控和审计保留期在系统设置中调整。

## 访问地址、HTTPS 与可信代理

参考 [Nginx 配置](../deploy/nginx.conf.example)，替换域名、证书及上游地址。Compose 仅发布 `127.0.0.1:8080`，外置 Nginx 与应用同机时代理到该地址，并保留原始 Host（含端口）。

系统设置中的“访问地址”对应原 APP_URL。留空时校验当前 Host 对应的浏览器来源，首次部署无需预填地址即可登录。生产建议设置固定的 HTTPS 访问源，包含非默认端口、不包含路径。保存后立即用于来源校验、登录 Cookie 和 OAuth2 回调地址。地址改变后，平台更新身份提供方的回调地址，还需到对应身份平台更新登记的地址。

“可信代理”对应原 TRUSTED_PROXIES，支持 IPv4、IPv6 和 CIDR 网段。留空时不信任转发头。仅填写实际反向代理的来源，用于登录限流和审计 IP，保存后立即生效。原 APP_URL 和 TRUSTED_PROXIES 环境变量不再读取，旧部署升级后在系统设置中填写。

## 备份与恢复

数据库和主密钥必须配套恢复，分别保管。数据库包含加密的云凭证和 OAuth2 Client Secret；32 字节主密钥在 `secrets/master.key`，无法用新密钥替代。

升级前停止应用写入，用 `pg_dump` 导出数据库，备份 `secrets/master.key`、`.env` 和代理配置。恢复时先导入数据库，再把对应密钥恢复到原目录并启动。不要使用 `docker compose down -v` 清理正在使用的数据库卷。

临时管理员密码仅首次写入服务日志，日志应限制访问。遗失临时密码时不要删除数据库或主密钥重新初始化。已有管理员可在用户管理中重置账号密码。

## 升级与回退

1. 备份数据库、主密钥和配置。
2. 停止 `api`，避免旧代码与迁移后的结构同时运行。
3. 拉取镜像或构建目标源码，需要固定版本时直接修改 Compose 中的镜像标签。
4. 使用所选 Compose 文件执行 `up -d`，自动迁移后检查 `/healthz` 和登录页。
5. 在系统设置中确认访问地址和可信代理，补齐云账号新增产品权限。

`000008_service_settings` 增加访问地址和可信代理设置。原有用户、密码和密钥保留，不会再次输出临时密码。此前的身份绑定、会话默认值、监控刷新和审计快照迁移仍由 golang-migrate 按顺序执行。

旧版独立 Worker 需先停止。回退前核对数据库兼容性，必要时恢复升级前数据库备份和对应密钥。程序只提供向上迁移命令，不应手工删除迁移记录绕过结构检查。

## 故障排查

| 现象                       | 检查方式                                                                                               |
| -------------------------- | ------------------------------------------------------------------------------------------------------ |
| 页面连接失败               | 检查应用进程、监听端口、`docker compose logs api` 和 `/healthz`；使用所选 Compose 文件对应的 `-f` 参数 |
| 页面返回“前端文件不可用”   | 使用内嵌前端的发布包，或按开发指南构建并启用 `with_frontend`                                           |
| 登录提示来源错误或会话失效 | 核对 系统设置中的访问地址、浏览器源、HTTPS 和代理配置，不混用主机名                                    |
| 启动提示数据库或迁移错误   | 检查 PostgreSQL 连接和服务日志；`bootstrap` 会自动执行迁移                                             |
| 启动提示主密钥缺失或不匹配 | 恢复对应密钥文件，检查路径和容器读取权限，不生成替代密钥                                               |
| 一直等待首次同步           | 检查账号启用状态、Worker 日志和云账号同步错误，点击手动同步；仅运行 `api` 不会处理任务                 |
| 数据过期但未更新           | 这是手动更新规则，页面不会自动回源，点击“更新”                                                         |
| 无数据库资源或同步鉴权失败 | 补齐 CAM/RAM 数据库查询权限，检查产品和地域可用性                                                      |
| 内存、磁盘监控不可用       | 区分无权限、无数据和采样过期，云端需已有监控数据                                                       |

应用日志使用 JSON 输出到标准输出。云接口失败输出 ERROR，字段包括 task_id、account_id、provider、site、region、service、action、error_code、权限 Action、request_id 和脱敏 message。地域发现、资源同步和监控刷新还输出对应阶段的错误记录。

在云账号页点击“查看日志”查看最近一次同步的逐项失败原因。容器部署可用 `docker compose logs --tail=200 api` 查看服务日志；使用额外 Compose 配置时保持相同的 `-f` 参数。二进制部署查看进程标准输出或对应服务管理器日志。敏感操作结果仍在审计页和实例详情查看。

`InvalidAccessKeyId` 表示云端无法识别当前凭证，应核对密钥是否启用、填写正确并属于对应账号。`Forbidden`、`UnauthorizedOperation` 等错误按提示检查实际 RAM/CAM Action 及资源范围。超时、DNS、限流和非法响应使用不同错误码。日志不保存凭证、Token、签名请求或完整响应正文。
