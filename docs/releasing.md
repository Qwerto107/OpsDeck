# 发布流程

[文档中心](README.md) · [部署指南](deployment.md) · [更新记录](changelog.md)

## 工作流

流程参照 [UptimeNest](https://github.com/Qwerto107/UptimeNest/blob/main/.github/workflows/ci.yml)，实现位于 [.github/workflows/ci.yml](../.github/workflows/ci.yml)。

| 触发方式                   | 执行内容                                                               |
| -------------------------- | ---------------------------------------------------------------------- |
| 分支推送、Pull Request     | Go 格式、静态检查、PostgreSQL 集成测试，前端类型、测试、格式检查和构建 |
| 手动执行                   | 常规检查及全部平台程序，可选择竞态、容器检查和镜像发布                 |
| 推送 `vX.Y.Z` 或预发布标签 | 增加可达依赖漏洞检查，构建程序和多架构镜像，发布 Release 附件          |

普通分支推送按工作流的 `paths` 过滤，部分文档变更不触发检查；版本标签不受路径过滤限制。Go 使用 1.26.10，Node.js 使用 22，集成测试使用 PostgreSQL 18。

## 产物

| 类型         | 平台及内容                                  |
| ------------ | ------------------------------------------- |
| Linux 程序   | amd64、arm64、armv6、armv5、loong64，tar.gz |
| Windows 程序 | amd64，ZIP                                  |
| macOS 程序   | arm64，tar.gz                               |
| 容器镜像     | Linux amd64、arm64、ARMv6，发布到 GHCR      |

前端构建一次后内嵌至各平台程序。压缩包包含运行说明，每个压缩包附 SHA-256 校验文件。镜像复用已经构建及校验的程序，不在目标架构中重复编译。

## 发布步骤

更新 `web/package.json`、`web/package-lock.json` 和 [更新记录](changelog.md)，完成本地检查。遵循项目提交授权规则，采用 Conventional Commits，再创建新标签。不要覆盖已推送的版本标签。

```sh
git tag -a v0.1.4 -m "release: v0.1.4"
git push origin main
git push origin v0.1.4
```

代码与标签推送完成后即可回报，不跟踪或等待 GitHub Actions。回报只能确认已经完成的操作；云端构建、镜像及 Release 结果以工作流为准。

正式版本构建成功后发布：

```text
ghcr.io/qwerto107/opsdeck:0.1.4
ghcr.io/qwerto107/opsdeck:latest
ghcr.io/qwerto107/opsdeck:sha-<完整提交SHA>
```

预发布只更新自身版本和提交标签，不更新 `latest`。手动镜像发布仅支持 main，生成提交标签。镜像使用方式见 [部署指南](deployment.md)。

## Release 与权限

工作流先创建草稿，检查全部压缩包和校验文件，上传后重新下载校验，成功后公开 Release。重跑复用已有附件并核对内容，不覆盖同名附件。

使用工作流自带的 `GITHUB_TOKEN`：镜像任务需要 `packages: write`，Release 任务需要 `contents: write`。供匿名部署使用的 GHCR 包需公开并正确关联仓库，无需提交云密钥或额外 PAT。

手动完整容器检查使用临时 PostgreSQL，执行迁移、初始化，再验证健康接口与前端入口。它不替代真实云账号及生产环境联调。
