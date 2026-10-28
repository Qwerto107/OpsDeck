# 数据库资源展示

[文档中心](README.md) · [使用指南](usage.md)

## 范围与使用

数据库页面展示腾讯云 MySQL、PostgreSQL，以及阿里云 RDS MySQL、PostgreSQL。国内站和国际站沿用云账号选择的站点。

列表显示实例 ID、名称、引擎和版本、所属账号、地域、状态、规格、存储容量、连接地址和标签。支持搜索、筛选和按名称、地域、标签设置多字段排序。详情显示基础配置和公网、内网连接地址。

页面进入和重载只读取快照。点击“更新”后复用对应账号的同步任务，完成后读取新快照，缓存有效期为 3 分钟。账号同步同时查询服务器和数据库。已有云账号需要补充以下查询权限，再手动更新。

平台不连接数据库、不获取数据库密码。数据库资源没有开关机、重启、配置变更或专属监控功能。API、Worker 和云适配层均拒绝将数据库当作服务器执行操作。

## 云端权限

腾讯云在原 CAM 查询策略中增加以下 Action，资源范围为 `*`：

```json
[
  "cdb:DescribeDBInstances",
  "postgres:DescribeRegions",
  "postgres:DescribeDBInstances"
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
  "rds:DescribeTags"
]
```

这些权限仅用于读取配置和标签，不包含数据库操作权限。平台仍按同一角色内的 `resources.read` 和云账号、资源类型、地域、实例范围授权。资源类型选择 MySQL 或 PostgreSQL；已有“全部类型”范围也覆盖新增数据库类型。

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
