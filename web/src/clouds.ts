export const resourceKinds = [
  { label: "CVM", value: "cvm" },
  { label: "Lighthouse", value: "lighthouse" },
  { label: "ECS", value: "ecs" },
  { label: "轻量应用服务器（阿里云）", value: "sas" },
];
export function kindLabel(kind: string) {
  return resourceKinds.find((item) => item.value === kind)?.label || kind;
}
export function cloudLabel(provider: string, site: string) {
  const name = provider === "alibaba" ? "阿里云" : "腾讯云";
  return site === "international" ? `${name}国际站` : name;
}
