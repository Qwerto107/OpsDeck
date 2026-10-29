<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NButton,
  NInput,
  NInputNumber,
  NCheckboxGroup,
  NCheckbox,
  NDataTable,
  NModal,
  NCard,
  NForm,
  NFormItem,
  NSwitch,
  NAlert,
  useMessage,
  useDialog,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Plus } from "lucide-vue-next";
import { api } from "../api";
import { useSession } from "../store";
import type { Provider, Settings } from "../types";
const session = useSession(),
  message = useMessage(),
  dialog = useDialog(),
  settings = reactive<Settings>({
    ...session.settings,
    metrics: [...session.settings.metrics],
    trusted_proxies: [...session.settings.trusted_proxies],
  }),
  providers = ref<Provider[]>([]),
  busy = ref(false),
  error = ref(""),
  modal = ref(false),
  providerBusy = ref(false);
let oldRetention = settings.retention_days;
const trustedProxies = ref(settings.trusted_proxies.join("\n"));
const blank = () => ({
  id: 0,
  name: "",
  client_id: "",
  client_secret: "",
  auth_url: "",
  token_url: "",
  user_url: "",
  redirect_url: "创建后自动生成",
  scopes: "openid profile",
  subject_field: "sub",
  enabled: true,
});
const form = reactive(blank()),
  metricOptions = [
    ["cpu", "CPU 使用率"],
    ["memory", "内存使用率"],
    ["disk", "磁盘空间使用率"],
    ["network_in", "网络入站"],
    ["network_out", "网络出站"],
  ];
async function load() {
  error.value = "";
  try {
    Object.assign(settings, await api("/settings"));
    trustedProxies.value = settings.trusted_proxies.join("\n");
    oldRetention = settings.retention_days;
    providers.value = await api("/providers");
  } catch (e) {
    error.value = (e as Error).message;
  }
}
async function commit(confirmed = false) {
  busy.value = true;
  try {
    const saved = await api<Settings>(
      "/settings",
      "PUT",
      {
        ...settings,
        trusted_proxies: trustedProxies.value
          .split(/[\n,]+/)
          .map((item) => item.trim())
          .filter(Boolean),
      },
      confirmed ? { "X-Confirm-Retention": "true" } : {},
    );
    session.settings = saved;
    Object.assign(settings, saved);
    trustedProxies.value = saved.trusted_proxies.join("\n");
    providers.value = await api("/providers");
    oldRetention = saved.retention_days;
    message.success("设置已保存");
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    busy.value = false;
  }
}
function save() {
  if (settings.retention_days < oldRetention) {
    dialog.warning({
      title: "缩短审计日志保留时间？",
      content: `早于 ${settings.retention_days} 天的日志将在后台清理，已有日志也受此策略影响。请确认已完成必要备份。`,
      positiveText: "确认并保存",
      negativeText: "取消",
      onPositiveClick: () => commit(true),
    });
  } else void commit();
}
function edit(row?: Provider) {
  Object.assign(form, blank(), row || {}, { client_secret: "" });
  modal.value = true;
}
async function saveProvider() {
  providerBusy.value = true;
  try {
    const result = await api<Provider>(
      "/providers" + (form.id ? "/" + form.id : ""),
      form.id ? "PUT" : "POST",
      form,
    );
    modal.value = false;
    message.success(
      `提供方已保存。回调地址应配置为 ${location.origin}/api/oauth/${result.id}/callback`,
    );
    await load();
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    providerBusy.value = false;
  }
}
const columns: DataTableColumns<Provider> = [
  { title: "提供方", key: "name" },
  { title: "授权端点", key: "auth_url", ellipsis: { tooltip: true } },
  {
    title: "状态",
    key: "enabled",
    width: 90,
    render: (r) => (r.enabled ? "已启用" : "已停用"),
  },
  {
    title: "操作",
    key: "actions",
    width: 80,
    render: (r) =>
      h(NButton, { size: "small", onClick: () => edit(r) }, () => "编辑"),
  },
];
onMounted(load);
</script>
<template>
  <n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert>
  <div class="settings-form">
    <section class="panel">
      <h2>访问与代理</h2>
      <p class="section-description">
        保存后立即生效，用于请求来源校验、登录 Cookie 和 OAuth2 回调地址。
      </p>
      <div class="form-line">
        <label for="app-url">访问地址</label>
        <div class="control">
          <n-input
            :input-props="{ id: 'app-url' }"
            v-model:value="settings.app_url"
            placeholder="https://opsdeck.example.com"
          />
          <div class="hint">
            留空时按当前访问地址校验。填写协议、域名和端口，不包含路径；修改后请同步更新身份平台中的
            OAuth2 回调地址。
          </div>
        </div>
      </div>
      <div class="form-line">
        <label for="trusted-proxies">可信代理</label>
        <div class="control">
          <n-input
            :input-props="{ id: 'trusted-proxies' }"
            v-model:value="trustedProxies"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="每行一个 IP 或 CIDR 网段"
          />
          <div class="hint">
            留空时不信任转发头。仅填写实际反向代理的 IPv4、IPv6 地址或 CIDR
            网段，用于登录限流和审计来源 IP。
          </div>
        </div>
      </div>
    </section>
    <section class="panel">
      <h2>监控设置</h2>
      <p class="section-description">
        平台级设置。用户在列设置中只能选择这里开启的指标。
      </p>
      <div class="form-line">
        <label>展示指标</label
        ><n-checkbox-group v-model:value="settings.metrics"
          ><div class="row wrap">
            <n-checkbox
              v-for="[key, label] in metricOptions"
              :key="key"
              :value="key"
              >{{ label }}</n-checkbox
            >
          </div></n-checkbox-group
        >
      </div>
      <div class="form-line">
        <label>数据过期提示</label>
        <div class="control">
          <div class="row">
            <n-input-number
              v-model:value="settings.stale_minutes"
              aria-label="数据过期提示（分钟）"
              :min="1"
              :max="1440"
              class="compact-number"
            /><span>分钟</span>
          </div>
          <div class="hint">
            默认 10
            分钟。超过此时间未取得新采样时高亮提示，仍保留旧数据；仅点击刷新按钮才请求云端，不会自动刷新。
          </div>
        </div>
      </div>
    </section>
    <section class="panel">
      <h2>登录会话</h2>
      <p class="section-description">
        可手动设置两项时限，任一到期都需重新登录。保存后对已有会话生效。
      </p>
      <div class="form-line">
        <label>无操作失效时间</label>
        <div class="control">
          <div class="row">
            <n-input-number
              v-model:value="settings.idle_minutes"
              aria-label="无操作失效时间（分钟）"
              :min="5"
              :max="1440"
              class="compact-number"
            /><span>分钟</span>
          </div>
          <div class="hint">
            默认 480 分钟（8 小时），支持 5 至 1,440 分钟。
          </div>
        </div>
      </div>
      <div class="form-line">
        <label>最长登录时长</label>
        <div class="row">
          <n-input-number
            v-model:value="settings.max_hours"
            aria-label="最长登录时长（小时）"
            :min="1"
            :max="168"
            class="compact-number"
          /><span>小时</span>
        </div>
      </div>
      <p class="hint">
        默认最长 72 小时，可设为 1 至 168
        小时。点击、键盘操作更新活动时间，后台同步和轮询不会续期。
      </p>
    </section>
    <section class="panel">
      <h2>审计日志保留</h2>
      <div class="form-line">
        <label>保留时间</label>
        <div class="control">
          <div class="row">
            <n-input-number
              v-model:value="settings.retention_days"
              :min="1"
              :max="3650"
              class="compact-number"
            /><span>天</span>
          </div>
          <div class="hint">
            默认 180 天。缩短保留时间后，已有日志将按新策略清理。
          </div>
        </div>
      </div>
    </section>
    <section class="panel">
      <div class="row spread">
        <h2>OAuth2 登录</h2>
        <n-button size="small" @click="edit()"
          ><Plus :size="14" style="margin-right: 4px" />添加提供方</n-button
        >
      </div>
      <p class="section-description">
        第三方身份必须关联已有平台账号，不会自动创建账号。
      </p>
      <n-data-table :columns="columns" :data="providers" :bordered="false" />
    </section>
    <div class="form-actions">
      <n-button :disabled="busy" @click="load">取消更改</n-button
      ><n-button type="primary" :loading="busy" @click="save"
        >保存更改</n-button
      >
    </div>
  </div>
  <n-modal v-model:show="modal" :mask-closable="!providerBusy"
    ><n-card
      style="width: 650px; max-width: 94vw; max-height: 90vh; overflow: auto"
      title="OAuth2 提供方"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><n-form @submit.prevent="saveProvider"
        ><n-form-item label="名称"
          ><n-input v-model:value="form.name"
        /></n-form-item>
        <div class="info-grid">
          <n-form-item label="Client ID"
            ><n-input v-model:value="form.client_id" /></n-form-item
          ><n-form-item label="Client Secret"
            ><n-input
              v-model:value="form.client_secret"
              type="password"
              placeholder="编辑时留空保持原值"
              autocomplete="new-password"
          /></n-form-item>
        </div>
        <n-form-item label="授权端点"
          ><n-input
            v-model:value="form.auth_url"
            placeholder="https://idp.example.com/authorize" /></n-form-item
        ><n-form-item label="令牌端点"
          ><n-input
            v-model:value="form.token_url"
            placeholder="https://idp.example.com/token" /></n-form-item
        ><n-form-item label="用户信息端点"
          ><n-input
            v-model:value="form.user_url"
            placeholder="https://idp.example.com/userinfo" /></n-form-item
        ><n-form-item label="回调地址"
          ><n-input :value="form.redirect_url" readonly
        /></n-form-item>
        <div class="info-grid">
          <n-form-item label="Scope（空格分隔）"
            ><n-input v-model:value="form.scopes" /></n-form-item
          ><n-form-item label="用户唯一标识字段"
            ><n-input
              v-model:value="form.subject_field"
              placeholder="sub 或 data.user_id"
          /></n-form-item>
        </div>
        <p class="hint">
          保存后自动生成本站回调地址，请将该地址配置到身份平台。自定义端点需使用
          HTTPS。
        </p>
        <n-form-item label="启用"
          ><n-switch v-model:value="form.enabled"
        /></n-form-item>
        <div class="form-actions">
          <n-button :disabled="providerBusy" @click="modal = false"
            >取消</n-button
          ><n-button type="primary" attr-type="submit" :loading="providerBusy"
            >保存提供方</n-button
          >
        </div></n-form
      ></n-card
    ></n-modal
  >
</template>
