<script setup lang="ts">
import { ref } from "vue";
import { NButton, NCheckbox, NPopover } from "naive-ui";
import { GripVertical, SlidersHorizontal } from "lucide-vue-next";

const props = defineProps<{
  choices: [string, string][];
  defaultOrder: string[];
  lastLabel: string;
  hint?: string;
}>();
const visible = defineModel<string[]>("visible", { required: true }),
  order = defineModel<string[]>("order", { required: true }),
  dragged = ref<string | null>(null);

function moveColumn(key: string, target: string) {
  const from = order.value.indexOf(key);
  const to = order.value.indexOf(target);
  if (from < 0 || to < 0 || from === to) return;
  const copy = [...order.value];
  copy.splice(from, 1);
  copy.splice(to, 0, key);
  order.value = copy;
}
function dropColumn(target: string) {
  if (dragged.value) moveColumn(dragged.value, target);
  dragged.value = null;
}
function nudgeColumn(key: string, delta: number) {
  const index = props.choices.findIndex(([k]) => k === key);
  const target = props.choices[index + delta];
  if (target) moveColumn(key, target[0]);
}
function resetColumns() {
  order.value = [...props.defaultOrder];
  visible.value = props.choices.map(([key]) => key);
}
function toggleColumn(key: string, value: boolean) {
  visible.value = value
    ? [...visible.value, key]
    : visible.value.filter((k) => k !== key);
}
</script>

<template>
  <n-popover trigger="click" placement="bottom-end">
    <template #trigger>
      <n-button class="push">
        <SlidersHorizontal :size="14" style="margin-right: 6px" />列设置
      </n-button>
    </template>
    <div class="columns-panel">
      <h3>列设置</h3>
      <p class="hint">拖动调整顺序，也可聚焦拖动柄后按上下键。{{ hint }}</p>
      <label><n-checkbox checked disabled>实例（固定）</n-checkbox></label>
      <div
        v-for="[key, label] in choices"
        :key="key"
        class="column-option"
        :class="{ dragging: dragged === key }"
        @dragover.prevent
        @drop.prevent="dropColumn(key)"
      >
        <button
          type="button"
          class="column-drag-handle"
          draggable="true"
          :aria-label="'调整' + label + '列顺序'"
          @dragstart="dragged = key"
          @dragend="dragged = null"
          @keydown.up.prevent="nudgeColumn(key, -1)"
          @keydown.down.prevent="nudgeColumn(key, 1)"
        >
          <GripVertical :size="15" />
        </button>
        <n-checkbox
          :checked="visible.includes(key)"
          @update:checked="(v) => toggleColumn(key, v)"
          >{{ label }}</n-checkbox
        >
      </div>
      <label
        ><n-checkbox checked disabled
          >{{ lastLabel }}（固定）</n-checkbox
        ></label
      >
      <n-button text type="primary" @click="resetColumns">恢复默认</n-button>
      <span class="muted small" style="float: right">自动保存</span>
    </div>
  </n-popover>
</template>

<style scoped>
.column-option {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  border-radius: 4px;
}
.column-option.dragging {
  background: #eef2ff;
}
.column-drag-handle {
  display: inline-flex;
  align-items: center;
  padding: 3px;
  color: #667085;
  background: transparent;
  border: 0;
  cursor: grab;
}
.column-drag-handle:active {
  cursor: grabbing;
}
.columns-panel .hint {
  max-width: 240px;
}
</style>
