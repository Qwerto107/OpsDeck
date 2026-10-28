import { createRouter, createWebHistory } from "vue-router";
import { useSession } from "./store";
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      component: () => import("./views/Login.vue"),
      meta: { title: "登录" },
    },
    { path: "/", redirect: "/servers" },
    {
      path: "/servers",
      component: () => import("./views/Servers.vue"),
      meta: { title: "服务器", permission: "resources.read" },
    },
    {
      path: "/servers/:id",
      component: () => import("./views/Detail.vue"),
      meta: { title: "实例详情", permission: "resources.read" },
    },
    {
      path: "/accounts",
      component: () => import("./views/Accounts.vue"),
      meta: { title: "云账号", permission: "accounts.manage" },
    },
    {
      path: "/access",
      component: () => import("./views/Access.vue"),
      meta: { title: "用户与权限", permission: "users.manage" },
    },
    {
      path: "/audits",
      component: () => import("./views/Audits.vue"),
      meta: { title: "审计日志", permission: "audit.read" },
    },
    {
      path: "/settings",
      component: () => import("./views/Settings.vue"),
      meta: { title: "系统设置", permission: "settings.manage" },
    },
    {
      path: "/profile",
      component: () => import("./views/Profile.vue"),
      meta: { title: "账号安全" },
    },
    { path: "/:pathMatch(.*)*", redirect: "/servers" },
  ],
});
router.beforeEach(async (to) => {
  const s = useSession();
  if (!s.ready) await s.load();
  if (!s.user && to.path !== "/login") return "/login";
  if (s.user && to.path === "/login") return "/servers";
  if (s.user?.must_change_password && to.path !== "/profile") return "/profile";
  if (to.meta.permission && !s.can(to.meta.permission as string))
    return "/profile";
  document.title = `${String(to.meta.title || "OpsDeck")} · OpsDeck`;
});
export default router;
