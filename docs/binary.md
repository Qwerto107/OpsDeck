# 二进制启动

## 下载与校验

从 [GitHub Releases](https://github.com/Qwerto107/OpsDeck/releases) 下载系统对应的压缩包及同名 `.sha256` 文件。支持 Linux amd64、arm64、armv6、armv5、loong64，Windows amd64 和 macOS arm64。

Windows 使用 ZIP，其余平台使用 tar.gz。程序已内嵌前端，不需要 Node.js 或外置前端目录；数据库仍需要 PostgreSQL。

Linux 校验及解压示例：

```sh
sha256sum -c opsdeck-linux-amd64.tar.gz.sha256
tar -xzf opsdeck-linux-amd64.tar.gz
cd opsdeck-linux-amd64
```

## 初始化与运行

以下为 Linux shell 示例。Windows PowerShell 使用 `$env:变量名='值'` 设置环境变量，程序名称为 `opsdeck.exe`。

```sh
export DATABASE_URL='postgres://opsdeck:你的数据库密码@127.0.0.1:5432/opsdeck?sslmode=disable'
export MASTER_KEY_FILE='./secrets/master.key'
export APP_URL='http://127.0.0.1:8080'
export LISTEN_ADDR='127.0.0.1:8080'
./opsdeck migrate

export INIT_USERNAME='admin'
export INIT_PASSWORD_FILE='./secrets/admin-password'
# 自行准备密码文件，密码为 12 至 72 字节。
./opsdeck init
# 初始化完成后安全移出初始密码文件。
./opsdeck all
```

`all` 同时运行 API 和 Worker，省略命令时默认使用 `all`。如需分别运行，可使用 `api` 和 `worker`；合并启动时无需再单独启动 Worker。

主密钥首次初始化时生成，必须持久保存并与数据库配套备份。现有数据库缺少密钥时不得重新生成密钥。`WEB_DIR` 可用于显式覆盖内嵌前端，正常使用发布包无需设置。

## 同步与访问

进入服务器列表或详情页、手动更新时触发资源同步；页面打开且可见时按设置间隔更新。Worker 不自主定时创建资源同步任务，仍执行已入队任务、操作结果核对和到期清理。

腾讯云接入权限见 [CAM 策略](https://github.com/Qwerto107/OpsDeck/blob/main/docs/tencent-cam.md)。生产访问使用 HTTPS，外置 Nginx 配置见 [部署示例](https://github.com/Qwerto107/OpsDeck/blob/main/deploy/nginx.conf.example)。完整配置及首次接入步骤见 [README](https://github.com/Qwerto107/OpsDeck/blob/main/README.md)。
