<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NButton,
  NDataTable,
  NTabs,
  NTabPane,
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NCheckboxGroup,
  NCheckbox,
  NAlert,
  useMessage,
  useDialog,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { Plus, Trash2 } from "lucide-vue-next";
import { api } from "../api";
import { useSession } from "../store";
import type { User, Role, Account, Scope, Identity, Provider } from "../types";
const session = useSession(),
  message = useMessage(),
  dialog = useDialog(),
  users = ref<User[]>([]),
  roles = ref<Role[]>([]),
  accounts = ref<Account[]>([]),
  identities = ref<Identity[]>([]),
  providers = ref<Provider[]>([]),
  busy = ref(false),
  error = ref(""),
  userModal = ref(false),
  roleModal = ref(false),
  saving = ref(false),
  temporary = ref("");
const userForm = reactive({
    id: 0,
    username: "",
    display_name: "",
    enabled: true,
    role_ids: [] as number[],
  }),
  roleForm = reactive({
    id: 0,
    name: "",
    permissions: [] as string[],
    scopes: [] as Scope[],
  });
const admin = () => session.user?.roles.some((r) => r.administrator);
const permissionLabels: Record<string, string> = {
  "resources.read": "查看资源",
  "monitor.read": "查看监控",
  "instances.start": "启动",
  "instances.stop": "正常关机",
  "instances.reboot": "正常重启",
  "instances.force_stop": "强制关机",
  "instances.force_reboot": "强制重启",
  "users.manage": "用户管理",
  "roles.manage": "角色管理",
  "accounts.manage": "云账号管理",
  "audit.read": "审计查询",
  "settings.manage": "系统设置",
};
async function load() {
  busy.value = true;
  error.value = "";
  try {
    users.value = await api("/users");
    if (admin()) {
      [roles.value, accounts.value, identities.value, providers.value] =
        await Promise.all([
          api<Role[]>("/roles"),
          api<Account[]>("/accounts"),
          api<Identity[]>("/identities"),
          api<Provider[]>("/providers"),
        ]);
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function editUser(u?: User) {
  Object.assign(userForm, {
    id: u?.id || 0,
    username: u?.username || "",
    display_name: u?.display_name || "",
    enabled: u?.enabled ?? true,
    role_ids: u?.roles.map((r) => r.id) || [],
  });
  userModal.value = true;
}
async function saveUser() {
  saving.value = true;
  try {
    const out = await api<{ temporary_password: string }>(
      "/users" + (userForm.id ? "/" + userForm.id : ""),
      userForm.id ? "PUT" : "POST",
      userForm,
    );
    userModal.value = false;
    temporary.value = out.temporary_password;
    message.success("用户已保存，现有会话已撤销");
    await load();
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    saving.value = false;
  }
}
function reset(u: User) {
  dialog.warning({
    title: `重置 ${u.username} 的密码？`,
    content: "现有会话将失效，用户下次登录必须修改临时密码。",
    positiveText: "重置密码",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const out = await api<{ temporary_password: string }>(
          `/users/${u.id}/reset-password`,
          "POST",
        );
        temporary.value = out.temporary_password;
      } catch (e) {
        message.error((e as Error).message);
      }
    },
  });
}
function deleteUser(u: User) {
  dialog.warning({
    title: `删除用户 ${u.username}？`,
    content: "用户的会话和第三方身份关联将被移除，审计记录保留。",
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/users/${u.id}`, "DELETE");
        message.success("用户已删除");
        await load();
      } catch (e) {
        message.error((e as Error).message);
      }
    },
  });
}
function editRole(r?: Role) {
  Object.assign(roleForm, {
    id: r?.id || 0,
    name: r?.name || "",
    permissions: [...(r?.permissions || [])],
    scopes: (r?.scopes || []).map((s) => ({ ...s })),
  });
  roleModal.value = true;
}
async function saveRole() {
  saving.value = true;
  try {
    await api(
      "/roles" + (roleForm.id ? "/" + roleForm.id : ""),
      roleForm.id ? "PUT" : "POST",
      roleForm,
    );
    roleModal.value = false;
    message.success("角色已保存，新权限即时生效");
    await load();
  } catch (e) {
    message.error((e as Error).message);
  } finally {
    saving.value = false;
  }
}
function deleteRole(r: Role) {
  dialog.warning({
    title: `删除角色 ${r.name}？`,
    content: "该角色将从所有用户移除，相关权限立即失效。",
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/roles/${r.id}`, "DELETE");
        message.success("角色已删除");
        await load();
      } catch (e) {
        message.error((e as Error).message);
      }
    },
  });
}
function unlink(row: Identity) {
  dialog.warning({
    title: "解除第三方身份关联？",
    content: "该用户的现有会话将失效，之后需要使用平台账号登录并重新绑定。",
    positiveText: "解除关联",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api("/identities/" + row.id, "DELETE");
        await load();
      } catch (e) {
        message.error((e as Error).message);
      }
    },
  });
}
const userColumns: DataTableColumns<User> = [
  { title: "账号", key: "username" },
  { title: "姓名", key: "display_name" },
  {
    title: "角色",
    key: "roles",
    render: (r) => r.roles?.map((x) => x.name).join("、") || "未分配",
  },
  {
    title: "状态",
    key: "enabled",
    render: (r) =>
      !r.enabled ? "已禁用" : r.must_change_password ? "待修改密码" : "正常",
  },
  {
    title: "操作",
    key: "actions",
    width: 220,
    render: (r) =>
      h("div", { class: "row" }, [
        h(
          NButton,
          { size: "small", disabled: !admin(), onClick: () => editUser(r) },
          () => "编辑",
        ),
        h(
          NButton,
          { size: "small", disabled: !admin(), onClick: () => reset(r) },
          () => "重置密码",
        ),
        h(
          NButton,
          {
            size: "small",
            quaternary: true,
            type: "error",
            disabled: !admin() || r.id === session.user?.id,
            "aria-label": `删除用户 ${r.username}`,
            title: "删除用户",
            onClick: () => deleteUser(r),
          },
          () => h(Trash2, { size: 15 }),
        ),
      ]),
  },
];
const roleColumns: DataTableColumns<Role> = [
  { title: "角色", key: "name" },
  {
    title: "功能权限",
    key: "permissions",
    render: (r) =>
      r.administrator
        ? "全部管理权限"
        : r.permissions.map((p) => permissionLabels[p] || p).join("、"),
  },
  {
    title: "资源范围",
    key: "scopes",
    render: (r) =>
      r.administrator
        ? "全部资源"
        : r.scopes
            .map(
              (s) =>
                `${accounts.value.find((a) => a.id === s.account_id)?.name || s.account_id} / ${s.kind || "全部类型"} / ${s.region || "全部地域"} / ${s.instance_id || "全部实例"}`,
            )
            .join("；") || "未授权资源",
  },
  {
    title: "操作",
    key: "actions",
    width: 120,
    render: (r) =>
      h("div", { class: "row" }, [
        h(
          NButton,
          {
            size: "small",
            disabled: r.administrator,
            onClick: () => editRole(r),
          },
          () => "编辑",
        ),
        h(
          NButton,
          {
            size: "small",
            quaternary: true,
            type: "error",
            disabled: r.administrator,
            "aria-label": `删除角色 ${r.name}`,
            title: "删除角色",
            onClick: () => deleteRole(r),
          },
          () => h(Trash2, { size: 15 }),
        ),
      ]),
  },
];
const identityColumns: DataTableColumns<Identity> = [
  {
    title: "用户",
    key: "user_id",
    render: (r) =>
      users.value.find((u) => u.id === r.user_id)?.username || r.user_id,
  },
  {
    title: "身份提供方",
    key: "provider_id",
    render: (r) =>
      providers.value.find((p) => p.id === r.provider_id)?.name ||
      r.provider_id,
  },
  { title: "第三方唯一标识", key: "subject" },
  {
    title: "操作",
    key: "actions",
    width: 100,
    render: (r) =>
      h(NButton, { size: "small", onClick: () => unlink(r) }, () => "解除关联"),
  },
];
onMounted(load);
</script>
<template>
  <n-alert v-if="error" type="error" class="error-banner">{{ error }}</n-alert
  ><n-tabs type="line" animated
    ><n-tab-pane name="users" tab="用户"
      ><div class="row spread" style="margin: 6px 0 18px">
        <span class="muted">账号由管理员创建，首次登录须修改临时密码。</span
        ><n-button type="primary" :disabled="!admin()" @click="editUser()"
          ><Plus :size="15" style="margin-right: 5px" />创建用户</n-button
        >
      </div>
      <div class="panel flush">
        <n-data-table
          :columns="userColumns"
          :data="users"
          :loading="busy"
          :bordered="false"
          :scroll-x="750"
        /></div></n-tab-pane
    ><n-tab-pane v-if="admin()" name="roles" tab="角色与权限"
      ><div class="row spread" style="margin: 6px 0 18px">
        <span class="muted">功能权限与该角色的资源范围共同生效。</span
        ><n-button type="primary" @click="editRole()"
          ><Plus :size="15" style="margin-right: 5px" />创建角色</n-button
        >
      </div>
      <div class="panel flush">
        <n-data-table
          :columns="roleColumns"
          :data="roles"
          :bordered="false"
          :scroll-x="850"
        />
      </div>
      <n-alert type="info"
        >例如：角色 A 可重启账号 A，角色 B 仅可查看账号
        B。拥有两个角色也不能重启账号 B。</n-alert
      ></n-tab-pane
    ><n-tab-pane v-if="admin()" name="identities" tab="第三方身份关联"
      ><div class="panel flush">
        <n-data-table
          :columns="identityColumns"
          :data="identities"
          :bordered="false"
        />
      </div>
      <p class="hint">
        绑定由用户在账号安全页主动发起，管理员仅可解除关联。
      </p></n-tab-pane
    ></n-tabs
  >
  <n-modal v-model:show="userModal" :mask-closable="!saving"
    ><n-card
      :title="userForm.id ? '编辑用户' : '创建用户'"
      style="width: 500px; max-width: 94vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><n-form @submit.prevent="saveUser"
        ><n-form-item label="账号"
          ><n-input
            v-model:value="userForm.username"
            placeholder="3～64 位字母、数字或 _ . @ -" /></n-form-item
        ><n-form-item label="姓名"
          ><n-input v-model:value="userForm.display_name" /></n-form-item
        ><n-form-item label="角色"
          ><n-select
            v-model:value="userForm.role_ids"
            multiple
            :options="
              roles.map((r) => ({ value: r.id, label: r.name }))
            " /></n-form-item
        ><n-form-item label="启用账号"
          ><n-switch v-model:value="userForm.enabled"
        /></n-form-item>
        <div class="form-actions">
          <n-button :disabled="saving" @click="userModal = false">取消</n-button
          ><n-button type="primary" attr-type="submit" :loading="saving"
            >保存用户</n-button
          >
        </div></n-form
      ></n-card
    ></n-modal
  >
  <n-modal v-model:show="roleModal" :mask-closable="!saving"
    ><n-card
      title="角色与资源授权"
      style="width: 750px; max-width: 94vw; max-height: 90vh; overflow: auto"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><n-form @submit.prevent="saveRole"
        ><n-form-item label="角色名称"
          ><n-input v-model:value="roleForm.name" /></n-form-item
        ><n-form-item label="功能权限"
          ><n-checkbox-group v-model:value="roleForm.permissions"
            ><div class="row wrap">
              <n-checkbox
                v-for="(label, p) in permissionLabels"
                :key="p"
                :value="p"
                >{{ label }}</n-checkbox
              >
            </div></n-checkbox-group
          ></n-form-item
        >
        <div class="row spread">
          <h3>资源范围</h3>
          <n-button
            size="small"
            @click="
              roleForm.scopes.push({
                account_id: accounts[0]?.id || 0,
                kind: '',
                region: '',
                instance_id: '',
              })
            "
            >添加范围</n-button
          >
        </div>
        <div v-for="(scope, i) in roleForm.scopes" :key="i" class="scope-row">
          <n-select
            v-model:value="scope.account_id"
            :options="accounts.map((a) => ({ label: a.name, value: a.id }))"
            placeholder="云账号"
            aria-label="资源范围云账号"
          /><n-select
            v-model:value="scope.kind"
            :options="[
              { label: '全部类型', value: '' },
              { label: 'CVM', value: 'cvm' },
              { label: 'Lighthouse', value: 'lighthouse' },
            ]"
            aria-label="资源类型"
          /><n-input
            v-model:value="scope.region"
            placeholder="地域代码，空表示全部"
            aria-label="授权地域"
          />
          <div class="row">
            <n-input
              v-model:value="scope.instance_id"
              placeholder="实例 ID，空表示全部"
              aria-label="授权实例"
            /><n-button
              quaternary
              aria-label="移除范围"
              @click="roleForm.scopes.splice(i, 1)"
              ><Trash2 :size="15"
            /></n-button>
          </div>
        </div>
        <p class="hint">
          指定实例时必须同时指定类型和地域。仅添加功能权限、未分配资源范围的角色不能访问服务器。平台管理权限不受服务器范围限制。
        </p>
        <div class="form-actions">
          <n-button :disabled="saving" @click="roleModal = false">取消</n-button
          ><n-button type="primary" attr-type="submit" :loading="saving"
            >保存角色</n-button
          >
        </div></n-form
      ></n-card
    ></n-modal
  >
  <n-modal :show="!!temporary" @update:show="(v) => !v && (temporary = '')"
    ><n-card
      title="请保存临时密码"
      style="width: 460px; max-width: 94vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      ><p class="muted">
        仅在本次操作后展示。请通过受信任渠道交给用户，用户首次登录后必须修改。
      </p>
      <n-input :value="temporary" readonly aria-label="临时密码" />
      <div class="form-actions" style="margin-top: 20px">
        <n-button type="primary" @click="temporary = ''">已保存，关闭</n-button>
      </div></n-card
    ></n-modal
  >
</template>
