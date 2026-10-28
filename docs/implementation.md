# 实现说明

## 目录

| 路径 | 职责 |
| --- | --- |
| cmd/opsdeck | migrate、init、all、api、worker 命令入口，默认使用 all |
| internal/app | HTTP 接口、认证授权、腾讯云适配、后台任务与测试 |
| migrations | 内嵌的版本化 SQL，不使用 AutoMigrate |
| web/src | Vue 页面、共享组件、Session 状态和接口调用 |
| deploy | 外置 Nginx 配置示例 |

## 接口

业务接口使用 `/api` 前缀。除登录、公开的提供方列表和授权回调外，均需 Session Cookie。写入接口需要 `X-CSRF-Token`，由登录或 `/me` 返回。

| 接口 | 用途 |
| --- | --- |
| POST /login；GET /me | 密码登录及当前用户 |
| POST /logout、/password、/activity | 注销、改密和记录用户活动 |
| PUT /preferences | 保存个人排序和列偏好 |
| GET /resources、/resources/:id | 授权资源列表与详情 |
| POST /resources/sync | 资源页面触发授权账号同步，返回任务编号 |
| GET /sync-tasks?ids=编号列表 | 查询有权访问的同步任务状态 |
| POST /resources/:id/actions | 创建单台实例操作任务 |
| GET、POST /accounts；PUT /accounts/:id | 云账号查询、新增和编辑 |
| POST /accounts/:id/sync | 请求同步 |
| GET、POST /users；PUT /users/:id | 用户管理 |
| POST /users/:id/reset-password | 生成临时密码并撤销会话 |
| GET、POST /roles；PUT /roles/:id | 角色及其资源范围 |
| GET /permissions | 权限点清单 |
| GET、PUT /settings | 平台设置 |
| GET、POST /providers；PUT /providers/:id | OAuth2 提供方配置 |
| GET /oauth/providers | 公开的登录入口 |
| GET /oauth/:id/start、/oauth/:id/callback | 授权及回调，link=true 表示绑定 |
| GET /identities；DELETE /identities/:id | 身份关联查询及管理员解绑 |
| GET /audits | 审计查询 |

完整路由以 `internal/app/server.go` 为准。错误响应为 `{ "error": "说明" }`。

## 数据与授权

资源由云平台、云账号、资源类型、地域和云端资源 ID 标识。服务器操作留在服务器模块。后续 LB 观测复用账号、授权和审计，再增加该产品的指标和详情结构。

同一角色中的功能权限与资源范围共同生效，再合并各角色的授权结果。平台管理权限不受服务器范围限制；用户、角色授权变更和解绑另要求管理员身份。

资源列表先应用授权、搜索和筛选，再对完整结果排序和分页。当前在应用内排序，后续根据实际数据规模评估数据库查询方案。

## 实施边界

默认由同一进程运行 API 和 Worker。启动前先绑定 HTTP 端口，绑定失败时不启动 Worker；退出时统一取消，并等待 HTTP 请求和 Worker 收尾后关闭数据库。Docker 镜像和 Compose 应用服务默认使用 `all`。

资源同步由页面进入、手动更新和可见页面的定时器触发，默认间隔 60 秒。POST /resources/sync 将任务入队，页面轮询任务状态，完成后重新读取数据库快照。相同账号的活跃同步任务通过唯一索引合并。

Worker 只领取已有任务，不再自主定时调度资源同步；操作结果核对和到期清理继续执行。关闭页面不会取消已入队任务。账号保存和手动账号同步也会入队。普通用户可触发其同一角色同时具有 resources.read 和账号范围的同步；详情刷新额外校验目标实例权限，查询结果继续应用完整资源范围。

- 页面均连接真实后端接口，没有生产模拟数据回退。
- 云 SDK 和 OAuth2 流程已实现，真实云账号和身份平台的兼容性仍需联调。
- 当前查询实例级磁盘空间指标，挂载点明细尚需结合云接口实际返回验证，页面显示明确的不可用状态。
- 通用 OAuth2 支持授权码、PKCE、自定义 HTTPS 端点和点分隔的用户唯一标识字段路径。厂商专有流程需根据实际文档适配。
- 支持停用云账号、用户和提供方；不提供物理删除账号或资源的接口。
- 仅保存当前资源快照，不提供监控历史趋势，不实现阿里云和 LB 接入。

## 验证环境

开发环境为 Windows，使用本机临时 PostgreSQL 18 验证迁移与关键接口。测试运行在独立 schema；界面检查使用本机明确标记的示例数据，不包含真实云密钥。

目标 Linux 的 Docker 镜像、Compose、外置 Nginx，以及真实云账号和身份平台需要环境联调。具体通过的检查以交付报告为准。

### 2026-10-28 检查结果

- 11 项 Go 测试通过，其中 4 项使用真实临时 PostgreSQL 测试认证、权限、排序、任务和密钥边界。
- go vet 通过，Windows 和 Linux amd64 二进制构建通过。
- 前端 4 项请求边界测试、TypeScript 检查、生产构建和 Prettier 校验通过。
- Compose YAML 可解析，服务清单及 API 回环地址绑定已检查；未执行 Docker 镜像构建或容器部署。
- 浏览器验证登录、服务器列表、多字段排序及刷新后的个人偏好保留。390 px 窄屏设置页没有页面级横向溢出。
- 界面示例云账号处于停用状态，不会执行云操作。截图和测试数据位于忽略目录 .local，不进入产品默认数据。
