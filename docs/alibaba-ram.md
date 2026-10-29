# 阿里云 RAM 接入权限

[文档中心](README.md) · [使用指南](usage.md)

## 接入方式

在云账号页面选择“阿里云”或“阿里云国际站”。国内站、国际站分别创建账号记录，使用对应 RAM 用户的 AccessKey ID 和 AccessKey Secret。平台加密保存凭证，编辑时留空表示保留原密钥。

适配器查询 ECS、轻量应用服务器和 RDS MySQL、PostgreSQL，按云端返回的地域同步资源。数据库只读展示配置、连接信息和当前资源监控，见 [数据库资源展示](database-resources.md)。站点用于账号标识和初始地域选择，不按地域名称推断账号站点。阿里云两站使用相同产品 API 协议和区域端点，权限仍由实际凭证决定。

已有账号不能切换厂商或站点，避免旧资源和权限范围关联到另一云账号。需要变更时新建账号记录。

## RAM 策略

以下策略对应当前代码的调用范围。查询覆盖全部地域；开关机和重启权限单列。只读接入时删除第二个 Statement，同时在平台分配只读角色。

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ecs:DescribeRegions",
        "ecs:DescribeInstances",
        "ecs:DescribeDisks",
        "rds:DescribeRegions",
        "rds:DescribeDBInstances",
        "rds:DescribeDBInstanceAttribute",
        "rds:DescribeDBInstanceNetInfo",
        "rds:DescribeTags",
        "rds:DescribeDBInstancePerformance",
        "swas-open:ListInstances",
        "cms:QueryMetricList",
        "cms:DescribeMetricMetaList"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "ecs:StartInstance",
        "ecs:StopInstance",
        "ecs:RebootInstance",
        "swas-open:StartInstance",
        "swas-open:StopInstances",
        "swas-open:RebootInstances"
      ],
      "Resource": "*"
    }
  ]
}
```

轻量服务器的 [ListRegions](https://www.alibabacloud.com/help/en/simple-application-server/developer-reference/api-swas-open-2020-06-01-listregions) 不需要授权，因此不向策略添加同名 Action。轻量关机和重启使用批量接口，每次只传一台实例。

平台不会创建、销毁或重装云服务器。ECS 关机显式使用 `StoppedMode=KeepCharging`，保留计算资源并继续计费，不自动切换为节省停机模式。强制关机、强制重启仍需平台权限及输入“确认”。

## 监控和验证

监控数据来自 CloudMonitor。ECS 使用 `acs_ecs_dashboard`，轻量服务器使用 `acs_swas`；轻量指标名称和单位根据云端元数据匹配。相同账号、地域、产品的查询按每批最多 50 台实例拆分，逐指标查询并处理分页，以实例 ID 匹配结果。

内存、磁盘空间使用率需要云端已有操作系统监控数据。平台不安装或管理 Agent。未采集、无权限和无数据分别显示为不可用或错误，不将磁盘 I/O 当作空间使用率。磁盘指标取最新采样时刻各挂载点的最高占用率。

接入后点击同步，检查地域、实例配置、标签及监控。云端没有返回标签字段时显示“标签获取失败”。验证开关机时使用已授权的测试实例，并核对平台审计与云端状态。请求结果不明时平台继续核对状态，不自动重复提交。

当前已通过模拟 API 和本地数据库测试；尚未使用真实阿里云国内站、国际站凭证联调。策略仍需在实际 RAM 控制台验证。

参考资料：

- [ECS StopInstance](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-stopinstance)
- [轻量 StopInstances](https://help.aliyun.com/en/simple-application-server/developer-reference/api-swas-open-2020-06-01-stopinstances)
- [轻量 RebootInstances](https://www.alibabacloud.com/help/tr/simple-application-server/developer-reference/api-swas-open-2020-06-01-rebootinstances)
- [CloudMonitor DescribeMetricList](https://help.aliyun.com/en/cms/cloudmonitor-1-0/developer-reference/api-cms-2019-01-01-describemetriclist)
- [云监控 Agent 与指标可用条件](https://www.alibabacloud.com/help/en/cms/cloudmonitor-1-0/user-guide/install-and-uninstall-the-cloudmonitor-agent-for-cpp)
