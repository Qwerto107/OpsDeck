# 数据库资源展示

[文档中心](README.md) · [使用指南](usage.md)

## 范围与使用

数据库页面展示腾讯云 MySQL、PostgreSQL，以及阿里云 RDS MySQL、PostgreSQL。国内站和国际站沿用云账号选择的站点。

列表显示实例 ID、名称、引擎和版本、所属账号、地域、状态、规格、存储容量、连接地址和标签。支持搜索、筛选和按名称、地域、标签设置多字段排序。详情显示基础配置和公网、内网连接地址。

页面进入和重载只读取快照。列表刷新当前页，详情刷新单台，采样偏旧提示默认 10 分钟。刷新中保留旧数据并显示耗时，失败项可查看原因并单独重试。新增资源仍由云账号同步发现；已有云账号需补充以下查询权限。

平台不连接数据库、不获取数据库密码。列表和详情显示 CPU、内存、存储空间使用率与当前连接数。数据库资源没有开关机、重启或配置变更功能。API、Worker 和云适配层均拒绝将数据库当作服务器执行操作。

## 云端权限

腾讯云在原 CAM 查询策略中增加以下 Action，资源范围为 `*`：

```json
[
  "cdb:DescribeDBInstances",
  "postgres:DescribeRegions",
  "postgres:DescribeDBInstances",
  "monitor:GetMonitorData"
]
```

MySQL 地域查询复用已有 `region:DescribeRegions`，参数为 `Product=cdb`。PostgreSQL 使用产品自身的 `DescribeRegions`。国际站使用对应服务的国际站端点。

阿里云在原 RAM 查询策略中增加以下 Action，资源范围为 `*`：

```json
[
  "rds:DescribeRegions",
  "rds:DescribeDBInstances",
  "rds:DescribeDBInstanceAttribute",
  "rds:DescribeDBInstanceNetInfo",
  "rds:DescribeTags",
  "rds:DescribeDBInstancePerformance"
]
```

这些权限仅用于读取配置、标签和监控，不包含数据库操作权限。指标还要求平台同一角色中的 monitor.read 与资源范围授权。平台仍按同一角色内的 `resources.read` 和云账号、资源类型、地域、实例范围授权。资源类型选择 MySQL 或 PostgreSQL；已有“全部类型”范围也覆盖新增数据库类型。

## 当前资源监控

| 产品                  | CPU / 内存 / 存储空间 / 连接数                                | 数据源                                                    |
| --------------------- | ------------------------------------------------------------- | --------------------------------------------------------- |
| 腾讯云 MySQL          | CpuUseRate / MemoryUseRate / VolumeRate / ThreadsConnected    | QCE/CDB，InstanceId 维度；云盘集群使用 Cluster 前缀       |
| 腾讯云 PostgreSQL     | Cpu / MemoryRate / StorageRate / Connections                  | QCE/POSTGRES，resourceId 维度                             |
| 阿里云 RDS MySQL      | MySQL_MemCpuUsage / MySQL_DetailedSpaceUsage / MySQL_Sessions | DescribeDBInstancePerformance，单次合并同一实例的多项指标 |
| 阿里云 RDS PostgreSQL | CpuUsage / MemoryUsage / PgSQL_SpaceUsage / PgSQL_Session     | DescribeDBInstancePerformance，单次合并同一实例的多项指标 |

百分比指标与连接数分别显示单位和采样时间。阿里云空间使用率由云端已用空间除以实例配置容量计算，未知容量或单位显示不可用，不把存储容量本身当作使用率。腾讯云按产品及地域批量查询，阿里云性能接口不支持跨实例批量查询。

只保存最新快照。超过 5 分钟的采样标记过期，查询失败或无采样明确显示原因，不显示为零。页面进入、重载及缓存过期不触发同步；点击同步更新后才重新获取。国内站及国际站复用相同业务流程，不安装 Agent，不提供历史趋势。

## 数据与边界

迁移 `000003_database_resources` 为资源表增加数据库展示字段。升级前运行 `migrate`，已有服务器快照继续保留。

数据库专有字段只保存引擎、版本、内存容量和地址列表。API 返回的其他安全配置、密码相关字段不会写入资源快照。阿里云配置和网络地址按实例查询；标签查询失败时保留资源并展示错误。配置或列表查询失败时，不把旧快照标记为云端不存在。

内存统一展示为 GB，保留低于 1 GB 的容量。未返回的配置显示为“—”。腾讯云 PostgreSQL 的已关闭或尚未开通地址不展示；云端状态无法识别时保留原值，不推断为运行中。

当前验证覆盖模拟云 API 字段映射、数据库字段持久化、资源分类、权限隔离及只读边界。真实云账号的权限、地域和数据库配置仍需联调。

## 官方参考

- [腾讯云 MySQL 查询实例](https://cloud.tencent.com/document/product/236/15872)
- [腾讯云 PostgreSQL 查询实例](https://cloud.tencent.com/document/product/409/16773)
- [腾讯云 PostgreSQL 数据结构](https://cloud.tencent.com/document/product/409/16778)
- [阿里云 RDS 查询实例](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstances)
- [阿里云 RDS 查询属性](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describedbinstanceattribute)
- [阿里云 RDS 查询标签](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-describetags)
