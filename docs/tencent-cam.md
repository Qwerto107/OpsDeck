# 腾讯云 CAM 接入策略

[文档中心](README.md) · [使用指南](usage.md)

## 适用范围

本文面向配置 OpsDeck 云账号的管理员。策略依据当前 [腾讯云适配代码](../internal/app/cloud.go) 整理，覆盖 CVM、轻量应用服务器（Lighthouse）、MySQL、PostgreSQL、地域发现和云监控。核对日期为 2026-10-28。

使用专门的 CAM 子用户创建 API 密钥，再把 Secret ID 和 Secret Key 填入 OpsDeck 的云账号页面。以下策略绑定到该子用户或其用户组，不绑定到 OpsDeck 的平台用户。

查询策略也包含 MySQL、PostgreSQL 的只读展示权限，字段范围与产品接口见 [数据库资源展示](database-resources.md)。数据库不需要下文的 CVM 或 Lighthouse 操作权限。

## 接口与权限

| 用途                                | CAM action                     | 说明                                                   |
| ----------------------------------- | ------------------------------ | ------------------------------------------------------ |
| CVM 地域发现                        | `region:DescribeRegions`       | 查询参数为 `Product=cvm`，当前代码使用地域管理系统接口 |
| Lighthouse 地域发现                 | 不在策略中声明                 | 调用 `DescribeRegions` API，实际鉴权行为待云端验证     |
| MySQL 地域发现                      | `region:DescribeRegions`       | 查询参数为 `Product=cdb`                               |
| MySQL 资源展示                      | `cdb:DescribeDBInstances`      | 只读取配置、连接信息和标签                             |
| PostgreSQL 地域发现                 | `postgres:DescribeRegions`     | 使用 PostgreSQL 产品接口                               |
| PostgreSQL 资源展示                 | `postgres:DescribeDBInstances` | 只读取配置、连接信息和标签                             |
| CVM 列表、详情和操作结果核对        | `cvm:DescribeInstances`        | 同时读取实例配置、IP、状态及返回的标签                 |
| Lighthouse 列表、详情和操作结果核对 | `lighthouse:DescribeInstances` | 同时读取实例配置、IP、状态及返回的标签                 |
| 两类实例的监控                      | `monitor:GetMonitorData`       | 查询 CPU、内存、磁盘空间和网络指标                     |
| CVM 启动                            | `cvm:StartInstances`           | 单台操作                                               |
| CVM 正常、强制关机                  | `cvm:StopInstances`            | 通过 `StopType` 区分                                   |
| CVM 正常、强制重启                  | `cvm:RebootInstances`          | 通过 `StopType` 区分                                   |
| Lighthouse 启动                     | `lighthouse:StartInstances`    | 单台操作                                               |
| Lighthouse 正常、强制关机           | `lighthouse:StopInstances`     | 通过 `StopType` 区分                                   |
| Lighthouse 正常、强制重启           | `lighthouse:RebootInstances`   | 通过 `StopType` 区分                                   |

地域管理系统的 [DescribeRegions](https://cloud.tencent.com/document/product/1596/77930) 与 [Lighthouse DescribeRegions](https://cloud.tencent.com/document/api/1207/55555) 是不同服务的接口，不能用 `cvm:DescribeRegions` 替代当前代码的 CVM 地域查询权限。

Lighthouse 的地域查询 API 存在，但截至核对日期，[官方 CAM 接口表](https://cloud.tencent.com/document/product/598/69972) 未列出 `DescribeRegions`。API 名称不能直接当作可授权的 CAM action；本策略不声明 `lighthouse:DescribeRegions`。上一版曾把它作为候选项放入 JSON，现已移除。

删除该项后，仍需通过实际调用验证 Lighthouse 地域查询。若接口返回鉴权失败，记录错误码与 RequestId，再向腾讯云确认支持的授权方式，不扩大到 `lighthouse:*`。CAM 策略创建校验与云 API 调用鉴权是两个验证步骤，不能用其中一个结果替代另一个。

当前代码从实例响应的 `Tags` 字段读取标签，没有单独调用标签服务，因此本策略不添加 `tag:*`。监控查询使用 `monitor:GetMonitorData`，不需要 `lighthouse:GetMonitorData`。

## 完整接入策略

建议名称为 `OpsDeckTencentAccess`。在 CAM 的自定义策略中选择 JSON 方式创建，粘贴以下内容，再关联到专用子用户。

策略允许当前账号全部地域、全部实例的查询及开关机操作，适用于已确认的“自动发现并同步全部可用地域”。它按接口收敛权限，但实例范围为全部；如需限制云端可操作实例，使用下一节的资源范围示例。

```json
{
  "version": "2.0",
  "statement": [
    {
      "effect": "allow",
      "action": [
        "region:DescribeRegions",
        "cdb:DescribeDBInstances",
        "postgres:DescribeRegions",
        "postgres:DescribeDBInstances",
        "cvm:DescribeInstances",
        "lighthouse:DescribeInstances",
        "monitor:GetMonitorData"
      ],
      "resource": ["*"]
    },
    {
      "effect": "allow",
      "action": [
        "cvm:StartInstances",
        "cvm:StopInstances",
        "cvm:RebootInstances"
      ],
      "resource": ["*"]
    },
    {
      "effect": "allow",
      "action": [
        "lighthouse:StartInstances",
        "lighthouse:StopInstances",
        "lighthouse:RebootInstances"
      ],
      "resource": ["*"]
    }
  ]
}
```

该策略不授予实例创建、销毁、重装、密码重置、安全组修改、标签修改或 CAM 管理权限。已有其他策略仍可能授予这些权限；以上 JSON 只描述本策略的授权内容。

只需要资源观测时，保留第一个 statement，删除后两个 statement。此时云端没有被本策略授予启动、关机或重启权限；OpsDeck 中也应分配只读角色。

## 限制实例操作范围

保留完整策略的第一个 statement，分别替换第二、第三个 statement 的 `resource`。按 [CVM 资源路径](https://cloud.tencent.com/document/product/213/10313) 和 [Lighthouse CAM 接口表](https://cloud.tencent.com/document/product/598/69972)，实例路径格式如下：

```text
qcs::cvm:<地域>:uin/<主账号UIN>:instance/<实例ID>
qcs::lighthouse:<地域>:uin/<主账号UIN>:instance/<实例ID>
```

例如，仅允许操作北京地域的一个 CVM 实例，第二个 statement 改为：

```json
{
  "effect": "allow",
  "action": ["cvm:StartInstances", "cvm:StopInstances", "cvm:RebootInstances"],
  "resource": ["qcs::cvm:ap-beijing:uin/<主账号UIN>:instance/ins-xxxxxxxx"]
}
```

将占位符替换为资源拥有者的主账号 UIN 和实际实例 ID。UIN 不是 APPID，也不是接入子用户的 UIN。Lighthouse 对应使用 `lighthouse` 和 `lhins-xxxxxxxx`。

允许指定地域的全部实例时，实例 ID 可改为 `*`；允许该账号所有地域的实例操作时，地域也可改为 `*`。需要多个实例或地域时，在 `resource` 数组中逐项列出。

此示例只收窄操作权限，查询仍覆盖全部资源。[GetMonitorData 是操作级授权接口](https://cloud.tencent.com/document/product/598/67487)，这里保留 `resource: ["*"]`，不能直接套用实例路径限制其监控查询范围。

## 云端权限与平台权限

CAM 决定接入密钥在腾讯云可以调用什么接口、操作哪些资源。OpsDeck 的用户角色决定谁可以通过平台使用这些能力，两层权限都必须满足。

普通与强制关机共用 `StopInstances`，普通与强制重启共用 `RebootInstances`，当前请求分别传入 `StopType=SOFT` 和 `StopType=HARD`。本策略没有把两种方式拆成独立云端权限；OpsDeck 另行校验 `instances.force_stop`、`instances.force_reboot`，并要求输入“确认”后执行强制操作。

云端 API 的关机参数见 [CVM StopInstances](https://cloud.tencent.com/document/api/213/15743) 和 [Lighthouse StopInstances](https://cloud.tencent.com/document/product/1207/47569)。

## 接入验证

1. 将策略关联到专用子用户，确认没有额外的全权限策略，然后在 OpsDeck 保存云账号凭证。
2. 保存启用的云账号后等待首次同步，或手动同步，检查是否发现各产品地域以及是否返回资源。应用需使用 `all`，或另行运行 Worker。
3. 检查监控指标。接口返回无数据与鉴权失败需要分别排查；增加 CAM 权限不会让云端未上报的指标出现。
4. 如需验证操作权限，在指定测试实例上由管理员执行已批准的操作，并确认平台审计与云端结果。

发生 `UnauthorizedOperation` 或 `AuthFailure` 时，核对失败接口、子用户绑定策略、显式拒绝和资源路径。CVM 地域查询失败时核对 `region:DescribeRegions`；Lighthouse 地域查询失败时按前文的 API 鉴权说明排查；监控请求失败时核对 `monitor:GetMonitorData`。不要把 Secret ID、Secret Key 或签名请求头写入排障记录。

本文已与当前代码调用清单及官方权限资料核对。修正版包含 13 个 action，JSON 已完成本地语法检查；尚未在真实 CAM 控制台导入或使用真实云账号验证。

## 腾讯云国际站

接入国际站账号时选择“腾讯云国际站”，使用国际站 CAM 凭证。CVM、Lighthouse、地域管理和监控复用本文的接口权限；请求端点按服务切换为 `<service>.intl.tencentcloudapi.com`。国内站账号继续使用原端点。

国际站接口及地域可用性由实际账号和云端返回决定。现有角色范围、强制确认、缓存与手动同步规则保持一致；当前尚未使用真实国际站凭证联调。

官方端点参考：[地域管理](https://indonesia.tencentcloud.com/document/product/1271/71925)、[监控](https://www.tencentcloud.com/document/product/248/39318)、[CVM 重启](https://intl.cloud.tencent.com/document/product/213/33243)。
