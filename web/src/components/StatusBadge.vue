<script setup lang="ts">
import { computed } from "vue";
const props = defineProps<{ value: string; label?: string }>();
const names: Record<string, string> = {
  RUNNING: "运行中",
  STOPPED: "已关机",
  STARTING: "启动中",
  STOPPING: "关机中",
  REBOOTING: "重启中",
  PENDING: "创建中",
  NOT_FOUND: "云端不存在",
  queued: "排队中",
  submitting: "请求提交中",
  waiting: "操作进行中",
  unknown: "结果待确认",
  succeeded: "已完成",
  success: "成功",
  failed: "失败",
  denied: "已拒绝",
  accepted: "已受理",
};
const tone = computed(() =>
  ["RUNNING", "succeeded", "success"].includes(props.value)
    ? "green"
    : ["failed", "denied", "unknown", "NOT_FOUND"].includes(props.value)
      ? "red"
      : [
            "STARTING",
            "STOPPING",
            "REBOOTING",
            "queued",
            "submitting",
            "waiting",
            "accepted",
          ].includes(props.value)
        ? "orange"
        : "gray",
);
</script>
<template>
  <span class="status" :class="tone"
    ><i aria-hidden="true" />{{ label || names[value] || value }}</span
  >
</template>
