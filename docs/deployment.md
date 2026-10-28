# 部署指南

[文档中心](README.md) · [二进制部署](binary.md) · [使用指南](usage.md)

## 部署方式

推荐在 Linux 上使用 Docker Compose，外置 Nginx 提供 HTTPS。应用在同一进程运行页面、API 和 Worker，PostgreSQL 保存配置、会话、资源快照、任务及审计。发布镜像和二进制均内嵌前端。

仓库的 `compose.yaml` 默认从源码构建。其 PostgreSQL 服务使用 18，以下卷路径也按该镜像组织。使用已有数据库时按目标环境配置 `DATABASE_URL`，不要直接替换数据库大版本镜像复用数据卷。

## 使用发布镜像

创建专用目录，下载 Compose 和环境变量模板：

```sh
mkdir opsdeck
cd opsdeck
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/compose.yaml -o compose.yaml
curl -fL https://raw.githubusercontent.com/Qwerto107/OpsDeck/main/.env.example -o .env
install -d -m 700 -o 10001 -g 10001 secrets
```

编辑 `.env`，设置 `POSTGRES_PASSWORD`、`APP_URL` 和 `INIT_USERNAME`。数据库密码建议使用随机十六进制字符串，避免连接 URL 的保留字符。在 `secrets/admin-password` 写入 12 至 72 字节的初始密码，设置所有者为 `10001:10001`、权限为 `600`。

创建 `compose.release.yaml`，指定要安装的已发布版本。以下以 `v0.1.2` 对应镜像为例；镜像需等待该版本工作流完成后才可拉取：

```yaml
services:
  migrate:
    image: ghcr.io/qwerto107/opsdeck:0.1.2
  init:
    image: ghcr.io/qwerto107/opsdeck:0.1.2
  api:
    image: ghcr.io/qwerto107/opsdeck:0.1.2
```

全部命令使用相同的 Compose 文件组合。`--no-build` 防止尝试从缺少源码的运行目录构建：

```sh
docker compose -f compose.yaml -f compose.release.yaml pull postgres migrate init api
docker compose -f compose.yaml -f compose.release.yaml up -d postgres
docker compose -f compose.yaml -f compose.release.yaml run --rm --no-build migrate
docker compose -f compose.yaml -f compose.release.yaml --profile setup run --rm --no-build init
docker compose -f compose.yaml -f compose.release.yaml up -d --no-build api
```

`init` 首次创建管理员和主密钥。初始化完成后安全移出初始密码文件，保留主密钥。已有用户时重复初始化会被拒绝，不会重置管理员。

容器使用 UID/GID `10001:10001`。Linux 目录按上述权限配置；Docker Desktop 的绑定挂载权限由宿主环境决定，确保容器用户可读密钥、仅初始化进程可写入即可。

## 从源码构建

```sh
git clone https://github.com/Qwerto107/OpsDeck.git
cd OpsDeck
cp .env.example .env
install -d -m 700 -o 10001 -g 10001 secrets
```

按上一节准备 `.env` 和初始密码文件，然后执行：

```sh
docker compose build
docker compose up -d postgres
docker compose run --rm migrate
docker compose --profile setup run --rm init
docker compose up -d api
```

源码方式也需要保留主密钥，初始化后移出密码文件。两种方式的服务名均为 `api`，该服务默认包含 Worker，无需再启动独立 Worker。

## 环境变量

| 变量                 | 用途及默认值                                                                        |
| -------------------- | ----------------------------------------------------------------------------------- |
| `DATABASE_URL`       | 必填，PostgreSQL 连接 URL；Compose 根据数据库密码生成                               |
| `MASTER_KEY_FILE`    | 主密钥路径，程序默认 `secrets/master.key`；容器为 `/run/opsdeck-secrets/master.key` |
| `APP_URL`            | 浏览器访问源；程序默认 `http://localhost:5173`，部署时必须显式设置                  |
| `LISTEN_ADDR`        | 程序默认 `127.0.0.1:8080`；容器内为 `0.0.0.0:8080`，宿主仅发布到回环地址            |
| `TRUSTED_PROXIES`    | 逗号分隔的可信代理 IP 或网段，默认不信任转发头                                      |
| `INIT_USERNAME`      | 首次初始化的管理员账号                                                              |
| `INIT_PASSWORD_FILE` | 首次初始化密码文件，优先于 `INIT_PASSWORD`                                          |
| `INIT_PASSWORD`      | 首次初始化密码，也可直接注入；推荐使用密码文件                                      |
| `WEB_DIR`            | 显式覆盖内嵌前端的目录，正常发布部署无需设置                                        |
| `POSTGRES_PASSWORD`  | Compose 中 PostgreSQL 的密码，不由应用直接读取                                      |

原生程序不会自动读取 `.env`，需由 shell 或服务管理器注入。数据库、主密钥、域名等部署配置用环境变量；监控展示、会话时长和审计保留期在后台设置。

## HTTPS 与反向代理

参考 [Nginx 配置](../deploy/nginx.conf.example)，替换域名、证书和上游地址。Compose 仅发布 `127.0.0.1:8080`，外置 Nginx 与应用同机时代理到该地址。

`APP_URL` 必须等于用户实际访问源，包括协议、域名及非默认端口。生产使用 HTTPS，应用据此设置 Secure Cookie。需要转发真实客户端 IP 时，`TRUSTED_PROXIES` 只填写实际代理来源。

本机试用可使用 `APP_URL=http://127.0.0.1:8080`。外部 PostgreSQL 连接按环境配置 TLS；Compose 示例使用私有容器网络。

## 备份与恢复

数据库和主密钥必须配套恢复，分别保管。数据库包含加密的云凭证、OAuth2 Client Secret 及业务数据；32 字节主密钥保存在指定文件中。已有加密数据或校验记录时，丢失密钥会阻止启动，不能重新生成替代密钥。

建议在升级前停止应用写入，使用 `pg_dump` 导出数据库，再安全备份 `secrets/master.key`、`.env` 及代理配置。恢复时先导入数据库，再恢复对应主密钥，核对权限后启动目标版本。

当前没有自动密钥轮换工具。直接替换主密钥无法解密已有凭证。保留初始密码文件不能代替主密钥备份。

## 升级与回退

1. 备份数据库、主密钥和部署配置。
2. 停止 `api`，避免旧代码与迁移后的结构同时运行。
3. 拉取目标镜像或构建目标源码。镜像方式修改覆盖文件中的 3 个应用版本，并使用上一节相同的 `-f` 参数。
4. 运行目标程序的 `migrate`，成功后启动 `api`，检查 `/healthz` 和登录页。
5. 已有云账号补齐新增产品权限，再手动同步。

`v0.1.2` 增加 `000002_cloud_sites` 和 `000003_database_resources`。旧账号默认国内站，已有服务器快照保留。数据库只读权限见 [数据库资源展示](database-resources.md)。升级时不要重新运行 `init`。

旧版若独立运行 Worker，切换到 `all` 前先停止旧 Worker。回退前核对数据库兼容性；必要时恢复升级前备份和对应密钥。程序只提供向上迁移命令，不应手工删除迁移记录绕过结构检查。

## 故障排查

| 现象                       | 检查方式                                                                                      |
| -------------------------- | --------------------------------------------------------------------------------------------- |
| 页面连接失败               | 检查应用进程、监听端口、`docker compose logs api` 和 `/healthz`；镜像部署使用同一组 `-f` 参数 |
| 页面返回“前端文件不可用”   | 使用内嵌前端的发布包，或按开发指南构建并启用 `with_frontend`                                  |
| 登录提示来源错误或会话失效 | 核对 `APP_URL`、浏览器源、HTTPS 和代理配置，不混用主机名                                      |
| 启动提示数据库或迁移错误   | 检查 PostgreSQL 连接，先运行目标版本 `migrate`                                                |
| 启动提示主密钥缺失或不匹配 | 恢复对应密钥文件，检查路径和容器读取权限，不生成替代密钥                                      |
| 一直等待首次同步           | 检查账号启用状态、Worker 日志和云账号同步错误，点击手动同步；仅运行 `api` 不会处理任务        |
| 数据过期但未更新           | 这是手动更新规则，页面不会自动回源，点击“更新”                                                |
| 无数据库资源或同步鉴权失败 | 补齐 CAM/RAM 数据库查询权限，检查产品和地域可用性                                             |
| 内存、磁盘监控不可用       | 区分无权限、无数据和采样过期，云端需已有监控数据                                              |

应用日志使用 JSON 输出到标准输出。同步状态在云账号页查看，敏感操作结果在审计页和实例详情查看。排障时保留请求 ID、错误码和时间，不复制密钥、密码或签名请求头。
