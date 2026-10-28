<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NButton,
  NDataTable,
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSwitch,
  NAlert,
  NEmpty,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Plus, Cloud, RefreshCw } from "lucide-vue-next";
import { api } from "../api";
import type { Account } from "../types";
const rows = ref<Account[]>([]),
  busy = ref(false),
  modal = ref(false),
  saving = ref(false),
  error = ref(""),
  message = useMessage(),
  editing = ref<number | null>(null);
const form = reactive({
  name: "",
  secret_id: "",
  secret_key: "",
  enabled: true,
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
  try {
    await api(`/accounts/${row.id}/sync`, "POST");
    message.success("同步任务已加入队列");
  } catch (e) {
    message.error((e as Error).message);
  }
}
const columns: DataTableColumns<Account> = [
  {
    title: "云账号",
    key: "name",
    render: (r) =>
      h("div", [
        h("b", r.name),
        h("div", { class: "subline" }, "腾讯云 · CVM / Lighthouse"),
      ]),
  },
  {
    title: "地域范围",
    key: "regions",
    render: (r) => (r.regions?.length ? r.regions.join("、") : "等待自动发现"),
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
          ? h("div", { class: "hint", style: "color:#b42318" }, r.sync_error)
          : null,
      ]),
  },
  {
    title: "操作",
    key: "actions",
    width: 180,
    render: (r) =>
      h("div", { class: "row" }, [
        h(NButton, { size: "small", onClick: () => edit(r) }, () => "编辑"),
        h(
          NButton,
          { size: "small", disabled: !r.enabled, onClick: () => sync(r) },
          () => "同步资源",
        ),
      ]),
  },
];
onMounted(load);
</script>
<template>
  <div class="row spread" style="margin-bottom: 20px">
    <div>
      <h1 class="page-title">云账号</h1>
      <p class="muted">集中管理腾讯云接入凭证，自动发现全部可用地域</p>
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
            >接入腾讯云账号后，自动发现并同步全部可用地域的 CVM 和
            Lighthouse。</template
          ></n-empty
        ></template
      ></n-data-table
    >
  </div>
  <p class="hint">
    密钥加密保存，编辑时留空表示保持原凭证。停用账号后停止同步和新操作，已有资源记录仍保留用于查询。
  </p>
  <n-modal v-model:show="modal" :mask-closable="!saving"
    ><n-card
      style="width: 550px; max-width: 94vw"
      :title="editing ? '编辑云账号' : '接入腾讯云账号'"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><n-form label-placement="top" @submit.prevent="save"
        ><n-form-item label="账号名称"
          ><n-input
            v-model:value="form.name"
            placeholder="例如：生产主账号"
            maxlength="100" /></n-form-item
        ><n-form-item label="Secret ID"
          ><n-input
            v-model:value="form.secret_id"
            :placeholder="editing ? '留空保持原凭证' : '腾讯云 Secret ID'"
            autocomplete="off" /></n-form-item
        ><n-form-item label="Secret Key"
          ><n-input
            v-model:value="form.secret_key"
            type="password"
            show-password-on="click"
            :placeholder="editing ? '留空保持原凭证' : '腾讯云 Secret Key'"
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
