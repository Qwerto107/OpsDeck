# API 参考

[文档中心](README.md) · [使用指南](usage.md) · [实现说明](implementation.md)

## 调用约定

业务路径以 `/api` 开头，使用 JSON。下表的 `:id` 为平台记录的数值 ID，云端实例 ID 使用 `instance_id` 字段。完整路由与数据结构以 [server.go](../internal/app/server.go) 和 [models.go](../internal/app/models.go) 为准。

登录后使用 `opsdeck_session` Cookie，不提供独立 API Token。`POST /login` 必须带与 `APP_URL` 一致的 `Origin`，成功返回 `user` 和 `csrf_token`。登录后的写请求必须带 `X-CSRF-Token`，可通过 `GET /me` 获取当前值。

错误返回 `{ "error": "说明" }`，常见状态为 400 参数错误、401 未登录或会话过期、403 无权限、404 不存在、409 冲突、429 登录限流和 503 服务不可用。响应头 `X-Request-ID` 用于排查问题。202 表示任务已受理，不代表云端操作完成。

## 会话与身份

| 方法与路径                | 用途                                             |
| ------------------------- | ------------------------------------------------ |
| `POST /login`             | 密码登录，请求字段 `username`、`password`        |
| `GET /me`                 | 当前用户及 CSRF Token                            |
| `POST /logout`            | 注销当前会话                                     |
| `POST /password`          | 修改密码                                         |
| `POST /activity`          | 记录当前用户活动                                 |
| `PUT /preferences`        | 保存个人排序和列偏好                             |
| `GET /oauth/providers`    | 获取公开登录入口，无需登录                       |
| `GET /oauth/:id/start`    | 发起第三方登录；`link=true` 用于已有登录用户绑定 |
| `GET /oauth/:id/callback` | OAuth2 回调                                      |
| `GET /identities`         | 查询身份关联                                     |
| `DELETE /identities/:id`  | 管理员解绑并撤销目标用户会话                     |

未关联的 OAuth2 身份不能登录，也不会自动创建用户。绑定必须由已有会话发起。

## 资源查询

`GET /resources` 返回 `items`、`total`、`page`、`page_size` 和 `cache`。

| 查询参数            | 内容                                                       |
| ------------------- | ---------------------------------------------------------- |
| `category`          | `server` 或 `database`，省略表示全部授权资源               |
| `account_id`        | 云账号 ID                                                  |
| `kind`              | `cvm`、`lighthouse`、`ecs`、`sas`、`mysql` 或 `postgresql` |
| `region`、`state`   | 地域代码、资源状态                                         |
| `q`                 | 实例 ID、名称和 IP 搜索；数据库还查询引擎、版本及连接地址  |
| `page`、`page_size` | 默认 1 和 20，每页最多 100                                 |
| `sort`              | URL 编码的 JSON 排序规则数组，最多 8 项                    |

排序规则示例：

```json
[
  { "field": "region", "desc": false },
  { "field": "tag", "key": "environment", "desc": false },
  { "field": "name", "desc": false }
]
```

排序字段为 `name`、`region`、`tag`；标签排序必须指定 `key`。默认按名称升序，同一字段或标签键不能重复。空值始终排在末尾，过滤和排序先于分页。

`GET /resources/:id` 返回 `resource`、最近最多 30 条 `tasks` 和 `cache`。`resource.allowed_actions` 为当前可执行操作；无监控权限时不返回指标数据。数据库的 `database` 字段包含 `engine`、`version`、`memory_gb` 和 `endpoints`。

`cache` 包含 `updated_at` 和固定的 `ttl_seconds: 180`。多账号取相关账号最早的成功更新时间，尚无完整成功记录时为 `null`。GET 请求和缓存过期均不触发云端同步。

## 同步任务

`POST /resources/sync` 请求示例：

```json
{ "account_id": 1 }
```

也可传 `resource_id`，额外校验该资源范围后同步所属账号。传空对象时同步用户有权刷新的启用账号。同步覆盖账号全部受支持产品，返回结果仍按用户资源范围过滤。

接口返回 202，格式为 `{ "tasks": [{ "id": 1, "status": "queued", "error": "" }] }`。同一账号有活跃任务时复用原任务。

`GET /sync-tasks?ids=1,2` 查询同步任务，每次最多 100 个 ID，返回 `tasks`。任务必须位于用户有权刷新的账号范围内。

## 服务器操作

`POST /resources/:id/actions` 创建单台操作任务，返回 202 和任务记录：

```json
{ "action": "reboot" }
```

`action` 支持 `start`、`stop`、`reboot`、`force_stop` 和 `force_reboot`。强制操作还必须提供 `"confirm": "确认"`。开机只允许 `STOPPED`，其他操作只允许 `RUNNING`，同时要求对应功能权限和资源范围。

同一实例有未完成任务时拒绝重复提交，数据库资源始终拒绝服务器操作。最终状态在资源详情的 `tasks` 中查看。`queued`、`submitting`、`waiting` 表示处理阶段，`unknown` 表示结果待确认，`succeeded`、`failed` 表示成功或失败。

## 管理接口

| 方法与路径                                                         | 用途及权限                                 |
| ------------------------------------------------------------------ | ------------------------------------------ |
| `GET /accounts`                                                    | 返回可见云账号                             |
| `POST /accounts`、`PUT /accounts/:id`                              | 新增或编辑，`accounts.manage`              |
| `DELETE /accounts/:id`                                             | 硬删除平台账号及快照，`accounts.manage`    |
| `POST /accounts/:id/sync`                                          | 手动账号同步，`accounts.manage`            |
| `GET /users`、`POST /users`、`PUT /users/:id`                      | 用户管理，`users.manage`；变更另校验管理员 |
| `DELETE /users/:id`、`POST /users/:id/reset-password`              | 删除用户或生成临时密码，管理员             |
| `GET /roles`、`POST /roles`、`PUT /roles/:id`、`DELETE /roles/:id` | 角色管理，`roles.manage`；变更另校验管理员 |
| `GET /permissions`                                                 | 可配置权限点，`roles.manage`               |
| `GET /settings`、`PUT /settings`                                   | 平台设置，写入要求 `settings.manage`       |
| `GET /providers`、`POST /providers`、`PUT /providers/:id`          | OAuth2 配置，`settings.manage`             |
| `GET /audits`                                                      | 审计查询，`audit.read`                     |

云账号字段为 `provider`（`tencent`、`alibaba`）、`site`（`china`、`international`）、`name`、`secret_id`、`secret_key` 和 `enabled`。厂商及站点创建后不能修改，凭证不在响应中返回。地域自动发现，不接受手工填写。

设置包括 `metrics`、`idle_minutes`、`max_hours`、`retention_days` 和兼容字段 `refresh_seconds`。缩短审计保留期需额外请求头 `X-Confirm-Retention: true`。`refresh_seconds` 不触发定时同步，具体范围见 [使用指南](usage.md#审计与系统设置)。

## 健康检查

`GET /healthz` 不带 `/api` 前缀，无需登录。数据库可连接时返回 200 和 `{ "status": "ok" }`，不可用时返回 503。健康检查不验证云端凭证或同步是否完成。
