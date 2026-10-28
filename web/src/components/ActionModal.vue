<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  NModal,
  NCard,
  NButton,
  NInput,
  NAlert,
  NCheckbox,
  useMessage,
} from "naive-ui";
import type { Resource } from "../types";
import { api } from "../api";
const props = defineProps<{
    show: boolean;
    resource: Resource | null;
    action: string;
  }>(),
  emit = defineEmits(["update:show", "done"]);
const labels: Record<string, string> = {
  start: "开机",
  stop: "关机",
  reboot: "重启",
  force_stop: "强制关机",
  force_reboot: "强制重启",
};
const forceAction = computed(() =>
    props.action === "stop" ? "force_stop" : "force_reboot",
  ),
  forceAllowed = computed(
    () => props.resource?.allowed_actions?.includes(forceAction.value) ?? false,
  ),
  forced = ref(false),
  effectiveAction = computed(() =>
    forced.value ? forceAction.value : props.action,
  ),
  name = computed(() => labels[effectiveAction.value] || effectiveAction.value),
  confirmation = ref(""),
  busy = ref(false),
  error = ref(""),
  message = useMessage();
watch(
  () => props.show,
  () => {
    confirmation.value = "";
    error.value = "";
    forced.value = false;
  },
);
async function submit() {
  if (!props.resource) return;
  busy.value = true;
  error.value = "";
  try {
    await api(`/resources/${props.resource.id}/actions`, "POST", {
      action: effectiveAction.value,
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
      <n-checkbox
        v-if="props.action !== 'start' && forceAllowed"
        v-model:checked="forced"
        :disabled="busy"
        style="margin-top: 16px"
        >{{ props.action === "stop" ? "强制关机" : "强制重启" }}</n-checkbox
      ><template v-if="forced"
        ><label class="field-label" style="margin-top: 16px"
          >请输入“确认”继续</label
        ><n-input
          v-model:value="confirmation"
          placeholder="确认"
          :disabled="busy"
          @keydown.enter="confirmation === '确认' && submit()" /></template
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
          :disabled="forced && confirmation !== '确认'"
          @click="submit"
          >确认{{ name }}</n-button
        >
      </div></n-card
    ></n-modal
  >
</template>
