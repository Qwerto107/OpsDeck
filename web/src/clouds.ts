export const resourceKinds = [
  { label: "CVM", value: "cvm" },
  { label: "Lighthouse", value: "lighthouse" },
  { label: "ECS", value: "ecs" },
  { label: "轻量应用服务器（阿里云）", value: "sas" },
];
export const databaseKinds = [
  { label: "MySQL", value: "mysql" },
  { label: "PostgreSQL", value: "postgresql" },
];
export const allResourceKinds = [...resourceKinds, ...databaseKinds];
export const databaseMetrics = [
  { key: "cpu", label: "CPU 使用率" },
  { key: "memory", label: "内存使用率" },
  { key: "disk", label: "存储空间使用率" },
  { key: "connections", label: "当前连接数" },
];
export function kindLabel(kind: string) {
  return allResourceKinds.find((item) => item.value === kind)?.label || kind;
}
export function cloudLabel(provider: string, site: string) {
  const name = provider === "alibaba" ? "阿里云" : "腾讯云";
  return site === "international" ? `${name}国际站` : name;
}
