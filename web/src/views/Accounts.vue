<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, reactive, ref } from "vue";
import {
  NButton,
  NDataTable,
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NAlert,
  NEmpty,
  useMessage,
  useDialog,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Plus, Cloud, RefreshCw, Trash2 } from "lucide-vue-next";
import { api } from "../api";
import type { Account } from "../types";
import { cloudLabel } from "../clouds";
const rows = ref<Account[]>([]),
  busy = ref(false),
  modal = ref(false),
  saving = ref(false),
  error = ref(""),
  message = useMessage(),
  dialog = useDialog(),
  editing = ref<number | null>(null);
const syncLog = ref<Account | null>(null),
  syncLogOpen = ref(false);
const pendingSync = ref<Record<number, number[]>>({});
let syncPoll: ReturnType<typeof setTimeout> | undefined;
let active = true;
let polling = false;
function showSyncLog(row: Account) {
  syncLog.value = row;
  syncLogOpen.value = true;
}
async function pollSync() {
  if (!active || polling) return;
  const ids = Object.values(pendingSync.value).flat();
  if (!ids.length) return;
  polling = true;
  try {
    const result = await api<{ tasks: { id: number; status: string }[] }>(
      "/sync-tasks?ids=" + ids.join(","),
    );
    if (!active) return;
    let finished = false;
    for (const [accountID, taskIDs] of Object.entries(pendingSync.value)) {
      if (
        taskIDs.every((id) =>
          result.tasks.some(
            (task) =>
              task.id === id && ["succeeded", "failed"].includes(task.status),
          ),
        )
      ) {
        delete pendingSync.value[Number(accountID)];
        finished = true;
      }
    }
    if (finished) await load();
  } catch (e) {
    if (!active) return;
    pendingSync.value = {};
    message.error("无法确认同步结果：" + (e as Error).message);
  } finally {
    polling = false;
  }
  if (active && Object.keys(pendingSync.value).length) {
    clearTimeout(syncPoll);
    syncPoll = setTimeout(() => void pollSync(), 2000);
  }
}
const form = reactive({
  provider: "tencent",
  site: "china",
  name: "",
  secret_id: "",
  secret_key: "",
  enabled: true,
});
const cloudOptions = [
  { label: "腾讯云", value: "tencent:china" },
  { label: "腾讯云国际站", value: "tencent:international" },
  { label: "阿里云", value: "alibaba:china" },
  { label: "阿里云国际站", value: "alibaba:international" },
];
const selectedCloud = computed({
  get: () => `${form.provider}:${form.site}`,
  set: (value: string) => {
    const [provider, site] = value.split(":");
    if (provider && site) {
      form.provider = provider;
      form.site = site;
    }
  },
});
async function load() {
  busy.value = true;
  error.value = "";
  try {
    rows.value = await api("/accounts");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function edit(row?: Account) {
  editing.value = row?.id ?? null;
  Object.assign(form, {
    provider: row?.provider || "tencent",
    site: row?.site || "china",
    name: row?.name || "",
    secret_id: "",
    secret_key: "",
    enabled: row?.enabled ?? true,
  });
  modal.value = true;
}
async function save() {
  saving.value = true;
  try {
    await api(
      "/accounts" + (editing.value ? "/" + editing.value : ""),
      editing.value ? "PUT" : "POST",
      form,
    );
    form.secret_id = "";
    form.secret_key = "";
    modal.value = false;
    message.success("云账号已保存，Worker 将同步资源");
    await load();
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    saving.value = false;
  }
}
async function sync(row: Account) {
  if (pendingSync.value[row.id]) return;
  try {
    const result = await api<{ tasks: { id: number }[] }>(
      `/accounts/${row.id}/sync`,
      "POST",
    );
    pendingSync.value[row.id] = result.tasks.map((task) => task.id);
    message.success("同步任务已加入队列");
    clearTimeout(syncPoll);
    syncPoll = setTimeout(() => void pollSync(), 1000);
  } catch (e) {
    message.error((e as Error).message);
  }
}
function remove(row: Account) {
  dialog.warning({
    title: `删除云账号 ${row.name}？`,
    content:
      "将删除平台中的凭证和资源快照，移除角色对应的授权范围。云端资源不受影响，审计和已完成任务保留。账号有未完成任务时无法删除。",
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/accounts/${row.id}`, "DELETE");
        message.success("云账号已删除");
        await load();
      } catch (e) {
        message.error((e as Error).message);
      }
    },
  });
}
const columns: DataTableColumns<Account> = [
  {
    title: "云账号",
    key: "name",
    render: (r) =>
      h("div", [
        h("b", r.name),
        h("div", { class: "subline" }, cloudLabel(r.provider, r.site)),
      ]),
  },
  {
    title: "地域范围",
    key: "regions",
    render: (r) =>
      r.regions?.length ? `已发现 ${r.regions.length} 个地域` : "等待自动发现",
  },
  {
    title: "状态",
    key: "enabled",
    render: (r) => (r.enabled ? "已启用" : "已停用"),
  },
  {
    title: "最近同步",
    key: "last_sync",
    render: (r) =>
      h("div", [
        r.last_sync ? new Date(r.last_sync).toLocaleString() : "等待首次同步",
        r.sync_error
          ? h("div", { class: "row", style: "color:#b42318;gap:8px" }, [
              h(
                "span",
                { class: "hint" },
                `同步失败 · ${r.sync_error.split("\n").filter(Boolean).length} 条错误`,
              ),
              h(
                NButton,
                {
                  text: true,
                  size: "small",
                  type: "error",
                  onClick: () => showSyncLog(r),
                },
                () => "查看日志",
              ),
            ])
          : null,
      ]),
  },
  {
    title: "操作",
    key: "actions",
    width: 220,
    render: (r) =>
      h("div", { class: "row" }, [
        h(NButton, { size: "small", onClick: () => edit(r) }, () => "编辑"),
        h(
          NButton,
          {
            size: "small",
            disabled: !r.enabled || !!pendingSync.value[r.id],
            loading: !!pendingSync.value[r.id],
            onClick: () => sync(r),
          },
          () => (pendingSync.value[r.id] ? "同步中…" : "同步资源"),
        ),
        h(
          NButton,
          {
            size: "small",
            quaternary: true,
            type: "error",
            "aria-label": `删除云账号 ${r.name}`,
            title: "删除云账号",
            onClick: () => remove(r),
          },
          () => h(Trash2, { size: 15 }),
        ),
      ]),
  },
];
onMounted(load);
onUnmounted(() => {
  active = false;
  clearTimeout(syncPoll);
});
</script>
<template>
  <div class="row spread" style="margin-bottom: 20px">
    <div>
      <h1 class="page-title">云账号</h1>
      <p class="muted">集中管理云账号接入凭证，自动发现全部可用地域</p>
    </div>
    <div class="row">
      <n-button quaternary :loading="busy" aria-label="刷新云账号" @click="load"
        ><RefreshCw :size="15" /></n-button
      ><n-button type="primary" @click="edit()"
        ><Plus :size="16" style="margin-right: 5px" />接入云账号</n-button
      >
    </div>
  </div>
  <n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert>
  <div class="panel flush">
    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="busy"
      :bordered="false"
      :scroll-x="850"
      ><template #empty
        ><n-empty description="尚未接入云账号" style="padding: 40px"
          ><template #icon><Cloud :size="34" /></template
          ><template #extra
            >接入云账号后，自动发现并同步可用地域的云服务器和轻量应用服务器。</template
          ></n-empty
        ></template
      ></n-data-table
    >
  </div>
  <p class="hint">
    密钥加密保存，编辑时留空表示保持原凭证。停用账号后停止同步和新操作，已有资源记录仍保留用于查询。
  </p>
  <n-modal v-model:show="syncLogOpen">
    <n-card
      class="sync-log-card"
      :title="`${syncLog?.name || '云账号'} · 同步日志`"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      aria-label="云账号同步日志"
      closable
      @close="syncLogOpen = false"
    >
      <p class="hint">
        {{
          syncLog?.last_sync
            ? new Date(syncLog.last_sync).toLocaleString()
            : "最近一次同步"
        }}
        ·
        按失败阶段、资源类型和地域列出；权限提示还需结合资源范围及显式拒绝检查。
      </p>
      <ol class="sync-log-lines">
        <li
          v-for="(line, index) in syncLog?.sync_error
            .split('\n')
            .filter(Boolean)"
          :key="index"
        >
          {{ line }}
        </li>
      </ol>
    </n-card>
  </n-modal>
  <n-modal v-model:show="modal" :mask-closable="!saving"
    ><n-card
      style="width: 550px; max-width: 94vw"
      :title="editing ? '编辑云账号' : '接入云账号'"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><n-form label-placement="top" @submit.prevent="save"
        ><n-form-item label="云厂商"
          ><n-select
            v-model:value="selectedCloud"
            :disabled="editing !== null"
            :options="cloudOptions" /></n-form-item
        ><n-form-item label="账号名称"
          ><n-input
            v-model:value="form.name"
            placeholder="例如：生产主账号"
            maxlength="100" /></n-form-item
        ><n-form-item
          :label="form.provider === 'alibaba' ? 'AccessKey ID' : 'Secret ID'"
          ><n-input
            v-model:value="form.secret_id"
            :placeholder="editing ? '留空保持原凭证' : '请输入云账号密钥 ID'"
            autocomplete="off" /></n-form-item
        ><n-form-item
          :label="
            form.provider === 'alibaba' ? 'AccessKey Secret' : 'Secret Key'
          "
          ><n-input
            v-model:value="form.secret_key"
            type="password"
            show-password-on="click"
            :placeholder="editing ? '留空保持原凭证' : '请输入云账号密钥'"
            autocomplete="new-password"
        /></n-form-item>
        <p class="hint">
          地域由 Worker
          自动获取并同步，无需手填。新增可用地域会在后续同步时自动纳入；现有角色资源权限仍然生效。
        </p>
        <n-form-item label="启用账号"
          ><n-switch v-model:value="form.enabled"
        /></n-form-item>
        <div class="form-actions">
          <n-button :disabled="saving" @click="modal = false">取消</n-button
          ><n-button attr-type="submit" type="primary" :loading="saving"
            >保存并同步</n-button
          >
        </div></n-form
      ></n-card
    ></n-modal
  >
</template>
<style scoped>
.sync-log-card {
  width: min(920px, calc(100vw - 32px));
  max-height: 82vh;
  overflow-y: auto;
}
.sync-log-lines {
  margin: 16px 0 0;
  padding-left: 24px;
}
.sync-log-lines li {
  padding: 12px 0;
  color: #8f1d14;
  line-height: 1.8;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border-bottom: 1px solid #eef0f4;
}
</style>
