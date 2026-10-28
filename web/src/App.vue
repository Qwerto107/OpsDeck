<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  NButton,
  NDropdown,
  zhCN,
  dateZhCN,
} from "naive-ui";
import {
  Server,
  Cloud,
  Users,
  ScrollText,
  Settings,
  Layers,
  Menu,
  ChevronDown,
  ShieldCheck,
} from "lucide-vue-next";
import { useSession } from "./store";
import { api } from "./api";
const session = useSession(),
  route = useRoute(),
  router = useRouter(),
  collapsed = ref(false);
const entries = [
  {
    to: "/servers",
    title: "服务器",
    icon: Server,
    permission: "resources.read",
  },
  {
    to: "/accounts",
    title: "云账号",
    icon: Cloud,
    permission: "accounts.manage",
  },
  {
    to: "/access",
    title: "用户与权限",
    icon: Users,
    permission: "users.manage",
  },
  {
    to: "/audits",
    title: "审计日志",
    icon: ScrollText,
    permission: "audit.read",
  },
  {
    to: "/settings",
    title: "系统设置",
    icon: Settings,
    permission: "settings.manage",
  },
];
const nav = computed(() => entries.filter((e) => session.can(e.permission)));
const theme = {
  common: {
    primaryColor: "#4f46e5",
    primaryColorHover: "#635bff",
    primaryColorPressed: "#4338ca",
    borderRadius: "8px",
    fontFamily: 'Inter, "PingFang SC", "Microsoft YaHei", sans-serif',
    fontSize: "13px",
    textColorBase: "#1b2433",
    placeholderColor: "#667085",
    textColor3: "#667085",
  },
  DataTable: {
    thColor: "#f9fafb",
    tdColor: "#ffffff",
    tdColorHover: "#fafaff",
    borderColor: "#f0f2f6",
    thFontWeight: "500",
  },
  Button: { fontWeight: "500" },
  Input: { border: "1px solid #e3e7ee" },
};
async function select(key: string) {
  if (key === "logout") {
    try {
      await session.logout();
    } finally {
      router.push("/login");
    }
  } else router.push("/profile");
}
function expired() {
  session.user = null;
  router.replace("/login?error=expired");
}
let lastActivity = 0;
function activity() {
  if (
    session.user &&
    !session.user.must_change_password &&
    Date.now() - lastActivity > 60000
  ) {
    lastActivity = Date.now();
    void api("/activity", "POST").catch(() => {});
  }
}
onMounted(() => {
  window.addEventListener("session-expired", expired);
  window.addEventListener("pointerdown", activity);
  window.addEventListener("keydown", activity);
});
onUnmounted(() => {
  window.removeEventListener("session-expired", expired);
  window.removeEventListener("pointerdown", activity);
  window.removeEventListener("keydown", activity);
});
</script>
<template>
  <n-config-provider
    :theme-overrides="theme"
    :locale="zhCN"
    :date-locale="dateZhCN"
    ><n-message-provider
      ><n-dialog-provider>
        <div v-if="route.path === '/login'" class="login-root">
          <router-view />
        </div>
        <div v-else class="app-shell" :class="{ 'nav-collapsed': collapsed }">
          <aside class="sidebar">
            <router-link to="/servers" class="brand"
              ><span class="brand-symbol"><Layers :size="19" /></span
              ><span>OpsDeck</span></router-link
            >
            <div class="nav-caption">工作空间</div>
            <nav>
              <router-link
                v-for="e in nav"
                :key="e.to"
                :to="e.to"
                :class="{ selected: route.path.startsWith(e.to) }"
                @click="collapsed = false"
                ><component :is="e.icon" :size="17" /><span>{{
                  e.title
                }}</span></router-link
              >
            </nav>
            <div class="sidebar-bottom">
              <ShieldCheck :size="16" /><span>公司内部云资源运维</span>
            </div>
          </aside>
          <div class="main-shell">
            <header class="topbar">
              <div class="row">
                <n-button
                  quaternary
                  class="menu-toggle"
                  aria-label="展开导航"
                  @click="collapsed = !collapsed"
                  ><Menu :size="18" /></n-button
                ><router-link
                  v-if="route.path.startsWith('/servers/')"
                  to="/servers"
                  class="muted"
                  >服务器 /</router-link
                ><b>{{ route.meta.title }}</b>
              </div>
              <n-dropdown
                :options="[
                  { label: '账号安全', key: 'profile' },
                  { label: '退出登录', key: 'logout' },
                ]"
                @select="select"
                ><button class="user-menu">
                  <span class="avatar">{{
                    (
                      session.user?.display_name ||
                      session.user?.username ||
                      "U"
                    ).slice(0, 1)
                  }}</span
                  ><span
                    >{{ session.user?.display_name || session.user?.username
                    }}<small>{{
                      session.user?.roles?.map((r) => r.name).join("、") ||
                      "未分配角色"
                    }}</small></span
                  ><ChevronDown :size="14" /></button
              ></n-dropdown>
            </header>
            <main class="page"><router-view /></main>
            <footer class="page-footer">
              OpsDeck <span>统一观测 · 审慎操作 · 全程留痕</span>
            </footer>
          </div>
        </div>
      </n-dialog-provider></n-message-provider
    ></n-config-provider
  >
</template>
