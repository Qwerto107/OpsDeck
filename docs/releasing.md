# 自动构建与发布

## 工作流

流程参照 [UptimeNest 的构建工作流](https://github.com/Qwerto107/UptimeNest/blob/main/.github/workflows/ci.yml)，实现位于 [.github/workflows/ci.yml](../.github/workflows/ci.yml)。

| 触发方式 | 执行内容 |
| --- | --- |
| 代码分支推送、Pull Request | Go 格式、静态检查、PostgreSQL 集成测试，以及前端类型检查、测试和构建 |
| 手动执行 | 在常规检查后构建全部平台程序，可选择竞态及容器检查、发布镜像 |
| 推送 `vX.Y.Z` 或预发布标签 | 增加可达依赖漏洞检查，构建程序、发布多架构镜像，最后公开 Release 附件 |

文档等不在工作流 paths 范围内的普通分支提交不会触发构建。标签推送不受 paths 过滤限制。

平台矩阵与参考项目一致：Linux amd64、arm64、armv6、armv5、loong64，Windows amd64、macOS arm64。前端先构建一次，再供各平台内嵌。压缩包携带运行说明和 SHA-256 文件。

镜像复用已构建、校验的程序，不在各目标架构内重复编译，支持 Linux amd64、arm64 和 ARMv6。保留本项目 Go 1.26.10、Node.js 22 和 PostgreSQL 技术栈。

## 发布步骤

完成验收并获得提交、推送授权后，提交代码并推送 main。确认常规检查通过，再推送版本标签：

```sh
git tag -a v0.1.0 -m "release: v0.1.0"
git push origin main
git push origin v0.1.0
```

标签构建成功后，发布以下镜像标签：

```text
ghcr.io/qwerto107/opsdeck:0.1.0
ghcr.io/qwerto107/opsdeck:latest
ghcr.io/qwerto107/opsdeck:sha-<完整提交SHA>
```

预发布版本只更新自身版本和提交标签，不更新 `latest`。手动发布镜像只能选择 main，生成提交标签。

Release 先创建为草稿。工作流检查全部压缩包及校验文件存在，上传附件，再下载服务器上的附件校验；全部通过后才公开 Release。重跑时复用已有附件并核对内容，不覆盖同名附件。

使用工作流自带的 `GITHUB_TOKEN`：镜像任务需要 `packages: write`，Release 任务需要 `contents: write`。首发后检查 GHCR 包的可见性及仓库关联；供匿名部署使用时应设为公开。无需向仓库提交云密钥或额外 PAT。

## 容器部署

从已发布镜像启动时，可通过 Compose 覆盖文件替换应用镜像：

```yaml
services:
  migrate:
    image: ghcr.io/qwerto107/opsdeck:0.1.0
  init:
    image: ghcr.io/qwerto107/opsdeck:0.1.0
  api:
    image: ghcr.io/qwerto107/opsdeck:0.1.0
```

保存为 `compose.release.yaml`，准备 `.env`、数据库和密钥目录后，按 [README 的初始化步骤](../README.md#linux-部署) 执行，并在每条命令中使用 `docker compose -f compose.yaml -f compose.release.yaml`。使用发布镜像时先执行 `pull`，启动或运行服务时增加 `--no-build`，无需本地构建。

API 与 Worker 同进程启动。PostgreSQL、主密钥文件及外置 Nginx 仍由部署环境提供。自动容器检查使用临时 PostgreSQL、执行迁移和初始化，再检查健康接口与前端入口。
