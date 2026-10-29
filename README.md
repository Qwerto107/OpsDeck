<div align="center">

# OpsDeck

可自托管的云运维平台。集中查看多云服务器和数据库资源，按角色授权执行服务器操作。

[快速部署](#快速部署) · [文档中心](docs/README.md) · [API 参考](docs/api.md) · [下载版本](https://github.com/Qwerto107/OpsDeck/releases)

**Go + Vue 3** · **PostgreSQL** · **腾讯云 / 阿里云**

一个应用进程同时提供页面、API 和任务处理，数据库使用 PostgreSQL。

</div>

---

## 功能

| 能力        | 使用方式                                                                       |
| ----------- | ------------------------------------------------------------------------------ |
| 多云接入    | 腾讯云、腾讯云国际站、阿里云和阿里云国际站，支持多个账号，自动发现全部可用地域 |
| 服务器      | 查看 CVM、Lighthouse、ECS 和轻量应用服务器的配置、IP、状态及标签               |
| 数据库资源  | 只读展示 MySQL、PostgreSQL 的配置、连接地址、标签和当前资源使用情况            |
| 搜索与排序  | 按账号、类型、地域和状态筛选，组合名称、地域和指定标签排序                     |
| 列设置      | 服务器列表支持列显隐和拖动顺序，保存个人偏好                                   |
| 当前监控    | 从云监控读取 CPU、内存、磁盘空间使用率和网络速率，显示采样时间及不可用原因     |
| 手动更新    | 当前页或单台刷新监控，显示采样时间、刷新耗时和逐实例结果，失败项可单独重试     |
| 服务器操作  | 单台开机、关机和重启，强制操作单独授权，并输入“确认”                           |
| 账号与权限  | 管理员创建账号，按用户、角色、功能权限及资源范围授权                           |
| OAuth2 登录 | 自定义提供方端点，第三方身份必须绑定已有账号，不自动创建用户                   |
| 审计与任务  | PostgreSQL 保存任务和操作结果，中文审计展示，日志按配置到期清理                |

## 快速部署

推荐使用 Linux、Docker Compose 和外置 Nginx。应用镜像包含前端，Compose 同时提供 PostgreSQL。以下命令从源码构建，使用发布镜像无需构建，见 [部署指南](docs/deployment.md#使用发布镜像)。

```sh
git clone https://github.com/Qwerto107/OpsDeck.git
cd OpsDeck
cp .env.example .env
install -d -m 700 -o 10001 -g 10001 secrets
```

编辑 `.env`，设置数据库密码、访问地址 `APP_URL` 和管理员名。在 `secrets/admin-password` 写入 12 至 72 字节的初始密码，设置所有者为 `10001:10001`、权限为 `600`。项目没有默认密码。

```sh
docker compose build
docker compose up -d postgres
docker compose run --rm migrate
docker compose --profile setup run --rm init
docker compose up -d api
```

1. 配置 [外置 Nginx](deploy/nginx.conf.example)，使访问地址与 `APP_URL` 一致。仅本机试用时可设为 `http://127.0.0.1:8080`。
2. 使用配置的管理员账号登录。初始化后安全移出初始密码文件，保留 `secrets/master.key`。
3. 按 [腾讯云 CAM](docs/tencent-cam.md) 或 [阿里云 RAM](docs/alibaba-ram.md) 配置权限，再添加云账号。
4. 等待首次同步完成，配置角色资源范围。后续在资源页面点击“更新”同步数据。

直接运行程序见 [二进制部署](docs/binary.md)。环境变量、HTTPS、备份和升级见 [部署指南](docs/deployment.md)。

## 支持范围

- 两家云厂商均支持国内站和国际站。界面提供 4 个云厂商选项，云账号创建后不能直接切换厂商或站点。
- 数据库资源仅展示 MySQL、PostgreSQL，不连接数据库，展示 CPU、内存、存储空间使用率和当前连接数，不提供数据库操作。
- 页面进入、重载和缓存过期均不触发同步。保存启用的云账号、手动账号同步和页面更新会触发账号同步。
- 仅展示当前云监控数据，不提供历史趋势。平台不安装 Agent 或通过 SSH 采集；内存、磁盘指标依赖云端已有数据。
- 删除云账号仅移除平台配置和快照，不销毁云资源。强制操作与正常操作分别校验权限。
- 真实云账号、国际站和身份平台的可用范围需结合目标环境联调。当前边界见 [实现说明](docs/implementation.md)。

## 文档导航

| 内容                             | 文档                                                                                              |
| -------------------------------- | ------------------------------------------------------------------------------------------------- |
| 云账号接入、资源查看、操作与授权 | [使用指南](docs/usage.md)                                                                         |
| Docker、环境变量、反向代理与升级 | [部署指南](docs/deployment.md)                                                                    |
| Linux、Windows、macOS 单文件运行 | [二进制部署](docs/binary.md)                                                                      |
| API 路由、参数与调用约束         | [API 参考](docs/api.md)                                                                           |
| 腾讯云、阿里云查询和操作权限     | [CAM 策略](docs/tencent-cam.md) · [RAM 策略](docs/alibaba-ram.md)                                 |
| 数据库展示范围与接入权限         | [数据库资源](docs/database-resources.md)                                                          |
| 本地开发、构建与验证             | [开发指南](docs/development.md)                                                                   |
| 技术栈、内部结构与业务约束       | [技术选型](docs/tech-stack.md) · [实现说明](docs/implementation.md) · [需求说明](requirements.md) |
| 二进制、多架构镜像与自动 Release | [发布流程](docs/releasing.md)                                                                     |
| 版本变化                         | [更新记录](docs/changelog.md)                                                                     |
