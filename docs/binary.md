# 二进制部署

[文档中心](https://github.com/Qwerto107/OpsDeck/blob/main/docs/README.md) · [部署指南](https://github.com/Qwerto107/OpsDeck/blob/main/docs/deployment.md)

## 下载与校验

从 [GitHub Releases](https://github.com/Qwerto107/OpsDeck/releases) 下载对应系统的压缩包及同名 `.sha256` 文件。支持 Linux amd64、arm64、armv6、armv5、loong64，Windows amd64 和 macOS arm64。

Windows 使用 ZIP，其余平台使用 tar.gz。程序内嵌前端，无需 Node.js 或外置前端目录；仍需准备 PostgreSQL。

Linux 校验与解压示例：

```sh
sha256sum -c opsdeck-linux-amd64.tar.gz.sha256
tar -xzf opsdeck-linux-amd64.tar.gz
cd opsdeck-linux-amd64
```

Windows 可使用 `Get-FileHash` 核对压缩包 SHA-256，再解压。macOS 可使用 `shasum -a 256 -c` 校验。

## 初始化与运行

以下使用 Linux shell。先准备 PostgreSQL 数据库，并确保运行用户可写入主密钥目录。临时密码首次初始化时自动生成，在服务日志中仅输出一次。

```sh
export DATABASE_URL='postgres://opsdeck:replace-with-db-password@127.0.0.1:5432/opsdeck?sslmode=disable'
export MASTER_KEY_FILE='./secrets/master.key'
export LISTEN_ADDR='127.0.0.1:8080'

export INIT_USERNAME='admin'
./opsdeck bootstrap
```

Windows 使用 `opsdeck.exe`，环境变量设置方式如下，其余变量按相同方式设置：

```powershell
$env:DATABASE_URL='postgres://opsdeck:replace-with-db-password@127.0.0.1:5432/opsdeck?sslmode=disable'
$env:MASTER_KEY_FILE='./secrets/master.key'
$env:INIT_USERNAME='admin'
.\opsdeck.exe bootstrap
```

管理员名默认 `admin`，临时密码自动生成，在初始化成功后日志的 `temporary_password` 字段中查看。首次登录必须修改密码。主密钥首次生成后持续复用，请与数据库配套备份。

后续使用相同命令启动，已有用户时不会重置密码。也可手动依次运行 `migrate`、`init` 和 `all`；`init` 同样生成临时密码，并拒绝重复初始化。

## 命令与配置

| 命令        | 用途                                             |
| ----------- | ------------------------------------------------ |
| `bootstrap` | 自动迁移、首次初始化，再运行 API 和 Worker       |
| `migrate`   | 执行内嵌的向上数据库迁移                         |
| `init`      | 首次生成主密钥及创建管理员                       |
| `all`       | 同进程运行 API 和 Worker，也是省略命令时的默认值 |
| `api`       | 只运行 HTTP 服务，不处理队列任务                 |
| `worker`    | 独立处理任务、操作结果核对和日志清理             |

合并模式无需另开 Worker。程序不自动读取 `.env`，环境变量需由 shell 或服务管理器注入。`WEB_DIR` 可显式覆盖内嵌前端，正常使用发布包无需设置。

主密钥与数据库分别备份、配套恢复。已有数据库缺少对应密钥时不能重新生成替代密钥。完整环境变量、HTTPS、备份及升级步骤见 [部署指南](https://github.com/Qwerto107/OpsDeck/blob/main/docs/deployment.md)。

## 接入与同步

云权限见 [腾讯云 CAM](https://github.com/Qwerto107/OpsDeck/blob/main/docs/tencent-cam.md) 和 [阿里云 RAM](https://github.com/Qwerto107/OpsDeck/blob/main/docs/alibaba-ram.md)。新增云账号后发现地域并首次同步，后续点击“更新”同步数据。

页面进入、重载和监控过期提示不触发云请求。过期提示默认 10 分钟，可在系统设置调整；列表刷新当前页，详情刷新单台。Worker 继续执行已入队任务和结果核对，不定时同步资源。数据库仅展示 MySQL、PostgreSQL 当前资源监控，不提供操作。
