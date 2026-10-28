<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { NModal, NCard, NButton, NInput, NAlert, useMessage } from "naive-ui";
import type { Resource } from "../types";
import { api } from "../api";
const props = defineProps<{
    show: boolean;
    resource: Resource | null;
    action: string;
  }>(),
  emit = defineEmits(["update:show", "done"]);
const labels: Record<string, string> = {
  start: "启动",
  stop: "正常关机",
  reboot: "正常重启",
  force_stop: "强制关机",
  force_reboot: "强制重启",
};
const name = computed(() => labels[props.action] || props.action),
  forced = computed(() => props.action.startsWith("force_")),
  confirmation = ref(""),
  busy = ref(false),
  error = ref(""),
  message = useMessage();
watch(
  () => props.show,
  () => {
    confirmation.value = "";
    error.value = "";
  },
);
async function submit() {
  if (!props.resource) return;
  busy.value = true;
  error.value = "";
  try {
    await api(`/resources/${props.resource.id}/actions`, "POST", {
      action: props.action,
      confirm: confirmation.value,
    });
    message.success("操作已受理，请在实例详情查看最终结果");
    emit("update:show", false);
    emit("done");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <n-modal
    :show="show"
    @update:show="(v) => !busy && emit('update:show', v)"
    :mask-closable="!busy"
    ><n-card
      style="width: 540px; max-width: calc(100vw - 32px)"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :aria-label="name + '确认'"
      ><div class="modal-title">{{ name }} {{ resource?.name }}？</div>
      <p class="muted mono">
        {{ resource?.instance_id }} · {{ resource?.kind.toUpperCase() }} ·
        {{ resource?.region }}
      </p>
      <div v-if="forced" class="danger-box">
        {{ name }}相当于直接断电{{
          action === "force_reboot" ? "后再启动" : ""
        }}，未落盘的数据可能丢失，文件系统可能损坏。请优先使用正常操作。
      </div>
      <p v-else class="muted">
        {{
          action === "start"
            ? "将启动此实例。"
            : "此操作会中断实例上的服务，请确认当前可以执行。"
        }}操作过程将记录审计日志。
      </p>
      <template v-if="forced"
        ><label class="field-label">请输入实例名称以确认</label
        ><n-input
          v-model:value="confirmation"
          :placeholder="resource?.name"
          :disabled="busy"
          @keydown.enter="
            confirmation === resource?.name && submit()
          " /></template
      ><n-alert
        v-if="error"
        type="error"
        class="error-banner"
        style="margin-top: 16px"
        >{{ error }}</n-alert
      >
      <div class="form-actions" style="margin-top: 24px">
        <n-button :disabled="busy" @click="emit('update:show', false)"
          >取消</n-button
        ><n-button
          :type="forced ? 'error' : 'primary'"
          :loading="busy"
          :disabled="forced && confirmation !== resource?.name"
          @click="submit"
          >确认{{ name }}</n-button
        >
      </div></n-card
    ></n-modal
  >
</template>
