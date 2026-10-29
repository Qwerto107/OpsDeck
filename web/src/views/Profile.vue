<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import {
  NForm,
  NFormItem,
  NInput,
  NButton,
  NAlert,
  NEmpty,
  useMessage,
} from "naive-ui";
import { Link, ShieldCheck } from "lucide-vue-next";
import { api } from "../api";
import { useSession } from "../store";
import type { Identity } from "../types";
const route = useRoute(),
  session = useSession(),
  message = useMessage(),
  password = reactive({ current: "", password: "", repeat: "" }),
  busy = ref(false),
  error = ref(""),
  identities = ref<Identity[]>([]),
  providers = ref<{ id: number; name: string }[]>([]);
async function load() {
  if (session.user?.must_change_password) return;
  try {
    [identities.value, providers.value] = await Promise.all([
      api<Identity[]>("/identities"),
      api<{ id: number; name: string }[]>("/oauth/providers"),
    ]);
  } catch (e) {
    error.value = (e as Error).message;
  }
}
async function change() {
  error.value = "";
  if (password.password !== password.repeat) {
    error.value = "两次输入的新密码不一致";
    return;
  }
  busy.value = true;
  try {
    await api("/password", "POST", password);
    Object.assign(password, { current: "", password: "", repeat: "" });
    await session.load();
    await load();
    message.success("密码已修改，其他设备的会话已退出");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="settings-form">
    <n-alert
      v-if="session.user?.must_change_password"
      type="warning"
      class="error-banner"
      >你正在使用临时密码，请先设置自己的密码，再访问其他页面。</n-alert
    ><n-alert v-if="error" type="error" class="error-banner">{{
      error
    }}</n-alert>
    <section class="panel">
      <h2 class="row"><ShieldCheck :size="18" />修改密码</h2>
      <p class="section-description">
        {{ session.user?.username }} · {{ session.user?.display_name }}
      </p>
      <n-form style="max-width: 440px" @submit.prevent="change"
        ><n-form-item label="当前密码"
          ><n-input
            v-model:value="password.current"
            type="password"
            autocomplete="current-password"
            show-password-on="click" /></n-form-item
        ><n-form-item label="新密码"
          ><n-input
            v-model:value="password.password"
            type="password"
            autocomplete="new-password"
            placeholder="12 至 72 字节，首尾不能有空白"
            show-password-on="click" /></n-form-item
        ><n-form-item label="再次输入新密码"
          ><n-input
            v-model:value="password.repeat"
            type="password"
            autocomplete="new-password"
            show-password-on="click" /></n-form-item
        ><n-button type="primary" attr-type="submit" :loading="busy"
          >修改密码</n-button
        ></n-form
      >
    </section>
    <section v-if="!session.user?.must_change_password" class="panel">
      <h2 class="row"><Link :size="18" />第三方身份关联</h2>
      <n-alert
        v-if="route.query.linked === 'true'"
        type="success"
        class="error-banner"
        >绑定成功，下次可直接使用对应第三方身份登录。</n-alert
      >
      <p class="section-description">
        可在这里主动绑定，也可从登录页发起第三方登录后按向导绑定已有账号。平台不会按姓名或邮箱自动关联。
      </p>
      <div v-for="p in providers" :key="p.id" class="keyline">
        <span>{{ p.name }}</span
        ><span
          v-if="
            identities.some(
              (i) => i.provider_id === p.id && i.user_id === session.user?.id,
            )
          "
          class="status green"
          >已关联</span
        ><n-button
          v-else
          tag="a"
          :href="'/api/oauth/' + p.id + '/start?link=true'"
          >绑定{{ p.name }}</n-button
        >
      </div>
      <n-empty
        v-if="!providers.length"
        description="管理员尚未配置身份提供方"
      />
      <p class="hint">
        需要解除关联时，请联系管理员。解除关联后会退出已有会话。
      </p>
    </section>
  </div>
</template>
