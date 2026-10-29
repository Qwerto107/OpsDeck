<script setup lang="ts">
import { computed } from "vue";
import { NTooltip } from "naive-ui";
import type { Metric } from "../types";
const props = defineProps<{ metric?: Metric; bar?: boolean }>();
const stale = computed(() => props.metric?.status === "stale");
const time = computed(() =>
  props.metric?.sampled_at
    ? new Date(props.metric.sampled_at).toLocaleString("zh-CN", {
        hour12: false,
      })
    : "无采样时间",
);
const label = computed(() =>
  props.metric?.value == null
    ? "不可用"
    : `${Number(props.metric.value.toFixed(2))}${props.metric.unit}`,
);
</script>
<template>
  <n-tooltip
    ><template #trigger
      ><div :class="metric?.value == null ? 'metric-na' : 'metric-value'">
        <span>{{ label }}</span
        ><span v-if="stale" class="subline"> · 数据过期</span>
        <div v-if="bar && metric?.value != null" class="meter">
          <i
            :class="{ high: metric.value >= 80 }"
            :style="{ width: Math.max(0, Math.min(100, metric.value)) + '%' }"
          />
        </div></div></template
    >{{ metric?.reason || "当前云监控数据" }}<br />{{ time }}</n-tooltip
  >
</template>
