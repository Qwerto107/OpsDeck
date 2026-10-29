<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { NAlert, NButton, NCheckbox, NInput, NSkeleton } from "naive-ui";
import { api } from "../api";
import { useSession } from "../store";

interface PendingBinding {
  provider_name: string;
  subject_hint: string;
  csrf_token: string;
  expires_at: string;
}

const router = useRouter(),
  session = useSession();
const pending = ref<PendingBinding>(),
  username = ref(""),
  password = ref(""),
  confirmed = ref(false),
  busy = ref(false),
  loading = ref(true),
  error = ref("");
let expiry: ReturnType<typeof setTimeout> | undefined;

async function load() {
  loading.value = true;
  error.value = "";
  clearTimeout(expiry);
  try {
    const data = await api<PendingBinding>("/oauth/bind");
    const remaining = Date.parse(data.expires_at) - Date.now();
    if (remaining <= 0) throw new Error("绑定请求已失效，请重新发起第三方登录");
    pending.value = data;
    expiry = setTimeout(() => {
      pending.value = undefined;
      password.value = "";
      confirmed.value = false;
      error.value = "绑定请求已失效，请返回登录页重新授权。";
    }, remaining);
  } catch (e) {
    pending.value = undefined;
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function bind() {
  if (!pending.value || !confirmed.value || busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    await api(
      "/oauth/bind",
      "POST",
      {
        username: username.value,
        password: password.value,
        confirm: confirmed.value,
      },
      {
        "X-OAuth-Bind-CSRF": pending.value.csrf_token,
      },
    );
    clearTimeout(expiry);
    await session.load();
    if (!session.user) throw new Error("绑定已完成，请返回登录页重新登录。");
    await router.replace("/servers");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    password.value = "";
    busy.value = false;
  }
}

watch(username, () => {
  confirmed.value = false;
});
onMounted(load);
onUnmounted(() => clearTimeout(expiry));
</script>

<template>
  <main class="binding-screen">
    <section class="panel binding-panel">
      <RouterLink to="/login" class="binding-brand">OpsDeck</RouterLink>
      <h1>绑定已有账号</h1>
      <p class="binding-description">
        第三方授权已完成。验证已有 OpsDeck 账号，确认绑定后即可登录。
      </p>
      <n-skeleton v-if="loading" height="240px" />
      <template v-else>
        <n-alert v-if="error" type="error" class="error-banner" role="alert">{{
          error
        }}</n-alert>
        <template v-if="pending">
          <dl class="binding-identity">
            <dt>登录方式</dt>
            <dd>{{ pending.provider_name }}</dd>
            <dt>第三方身份</dt>
            <dd>{{ pending.subject_hint }}</dd>
          </dl>
          <form class="binding-form" @submit.prevent="bind">
            <label for="binding-username"
              >OpsDeck 账号
              <n-input
                v-model:value="username"
                :input-props="{ id: 'binding-username' }"
                placeholder="管理员创建的已有账号"
                autocomplete="username"
                :disabled="busy"
              />
            </label>
            <label for="binding-password"
              >OpsDeck 密码
              <n-input
                v-model:value="password"
                :input-props="{ id: 'binding-password' }"
                type="password"
                placeholder="请输入该平台账号的密码"
                autocomplete="current-password"
                :disabled="busy"
              />
            </label>
            <n-checkbox
              v-model:checked="confirmed"
              :disabled="busy || !username"
              >我确认将此第三方身份绑定到账号“{{
                username || "待填写"
              }}”</n-checkbox
            >
            <n-button
              type="primary"
              attr-type="submit"
              size="large"
              block
              :loading="busy"
              :disabled="!username || !password || !confirmed"
              >验证账号并绑定</n-button
            >
          </form>
          <p class="hint">
            请求在授权后 5
            分钟内有效。仅绑定管理员创建的已有账号，不会创建新用户或按姓名、邮箱匹配账号。
          </p>
        </template>
        <div v-else class="binding-retry">
          <n-button @click="load">重试读取请求</n-button>
        </div>
        <RouterLink to="/login" class="binding-back"
          >返回账号密码登录</RouterLink
        >
      </template>
    </section>
  </main>
</template>

<style scoped>
.binding-screen {
  display: grid;
  place-items: center;
  min-height: 100svh;
  padding: 24px;
  background: #f5f6fa;
}
.binding-panel {
  width: 100%;
  max-width: 520px;
  margin: 0;
  padding: 32px;
}
.binding-brand {
  color: #4f46e5;
  font-size: 20px;
  font-weight: 700;
}
h1 {
  margin: 24px 0 12px;
  font-size: 24px;
}
.binding-description {
  color: #667085;
  margin: 0 0 24px;
  line-height: 1.7;
}
.binding-identity {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 10px;
  padding-bottom: 20px;
  margin: 0 0 20px;
  border-bottom: 1px solid #e3e7ee;
}
.binding-identity dt {
  color: #667085;
}
.binding-identity dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.binding-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.binding-form label {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.hint {
  margin-top: 16px;
}
.binding-back {
  display: inline-block;
  margin-top: 20px;
  color: #4f46e5;
}
.binding-retry {
  margin: 16px 0;
}
@media (max-width: 560px) {
  .binding-screen {
    padding: 16px;
  }
  .binding-panel {
    padding: 24px;
  }
}
</style>
