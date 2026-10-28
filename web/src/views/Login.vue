<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NInput, NButton, NAlert } from "naive-ui";
import { useSession } from "../store";
import { api } from "../api";
const inputTheme = { heightMedium: "34px", paddingMedium: "0 10px" };
const username = ref(""),
  password = ref(""),
  busy = ref(false),
  error = ref(""),
  providers = ref<{ id: number; name: string }[]>([]),
  router = useRouter(),
  route = useRoute(),
  session = useSession();
onMounted(async () => {
  if (route.query.error === "oauth")
    error.value = "第三方授权失败或身份尚未关联，请使用账号密码登录后绑定。";
  if (route.query.error === "expired") error.value = "会话已失效，请重新登录。";
  try {
    providers.value = await api("/oauth/providers");
  } catch {
    error.value = "暂时无法连接服务，请检查后端状态后重试。";
  }
});
async function login() {
  if (!username.value || !password.value) {
    error.value = "请输入账号和密码";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await session.login(username.value, password.value);
    password.value = "";
    router.push(session.user?.must_change_password ? "/profile" : "/servers");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="login-screen">
    <section class="login-brand-panel" aria-label="OpsDeck 云运维平台">
      <b class="login-wordmark">OpsDeck</b>
      <div>
        <h1>One deck,<br />every cloud.</h1>
        <p class="login-description">公司内部云资源运维平台</p>
      </div>
      <span class="login-capabilities">服务器 · 监控 · 操作 · 审计</span>
    </section>
    <section class="login-form-panel">
      <form class="login-form" @submit.prevent="login">
        <h2>登录</h2>
        <n-alert v-if="error" type="error" role="alert">{{ error }}</n-alert>
        <label class="login-field" for="login-username">
          账号
          <n-input
            v-model:value="username"
            :input-props="{ id: 'login-username' }"
            :theme-overrides="inputTheme"
            placeholder="请输入账号"
            autocomplete="username"
            :disabled="busy"
          />
        </label>
        <label class="login-field" for="login-password">
          密码
          <n-input
            v-model:value="password"
            :input-props="{ id: 'login-password' }"
            :theme-overrides="inputTheme"
            type="password"
            placeholder="请输入密码"
            autocomplete="current-password"
            :disabled="busy"
          />
        </label>
        <n-button attr-type="submit" type="primary" block :loading="busy"
          >登录</n-button
        >
        <template v-if="providers.length">
          <div class="login-alternative">或</div>
          <n-button
            v-for="p in providers"
            :key="p.id"
            tag="a"
            :href="'/api/oauth/' + p.id + '/start'"
            block
            >使用{{ p.name }}登录</n-button
          >
        </template>
        <span class="login-help">账号由管理员创建，忘记密码请联系管理员。</span>
      </form>
    </section>
  </div>
</template>

<style scoped>
.login-screen {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  min-height: 100svh;
  background: #fff;
  color: #1b2433;
  line-height: normal;
}

.login-brand-panel {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 64px;
  background: #312e81;
  color: #fff;
}

.login-wordmark {
  font-size: 22px;
  font-weight: 700;
}

.login-brand-panel h1 {
  margin: 0;
  font-size: 40px;
  font-weight: 600;
  line-height: 1.2;
  letter-spacing: normal;
}

.login-description {
  margin: 16px 0 0;
  color: #d6d9f5;
  font-size: 15px;
}

.login-capabilities {
  color: #d6d9f5;
}

.login-form-panel {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 0;
  padding: 32px;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
  width: 360px;
  max-width: 100%;
  margin: 0;
}

.login-form h2 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
}

.login-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.login-form :deep(.n-button) {
  height: 40px;
  font-weight: 400;
}

.login-alternative,
.login-help {
  color: #5b6678;
}

.login-alternative {
  text-align: center;
}

@media (max-width: 720px) {
  .login-screen {
    grid-template-columns: 1fr;
    grid-template-rows: auto 1fr;
  }

  .login-brand-panel {
    gap: 28px;
    padding: 32px;
  }

  .login-brand-panel h1 {
    font-size: 32px;
  }

  .login-form-panel {
    padding: 40px 24px;
  }
}
</style>
