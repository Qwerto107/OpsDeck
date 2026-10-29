import { defineStore } from "pinia";
import { ref } from "vue";
import { api, setCSRF } from "./api";
import type { User, Settings } from "./types";

export const useSession = defineStore("session", () => {
  const user = ref<User | null>(null),
    ready = ref(false);
  const settings = ref<Settings>({
    stale_minutes: 10,
    idle_minutes: 480,
    max_hours: 72,
    retention_days: 180,
    metrics: ["cpu", "memory", "disk", "network_in", "network_out"],
  });
  function can(permission: string) {
    return !!user.value?.roles?.some(
      (r) => r.administrator || r.permissions.includes(permission),
    );
  }
  async function load() {
    try {
      const data = await api<{ user: User; csrf_token: string }>("/me");
      user.value = data.user;
      setCSRF(data.csrf_token);
      if (!user.value.must_change_password)
        settings.value = await api<Settings>("/settings");
    } catch {
      user.value = null;
    } finally {
      ready.value = true;
    }
  }
  async function login(username: string, password: string) {
    const data = await api<{ user: User; csrf_token: string }>(
      "/login",
      "POST",
      { username, password },
    );
    user.value = data.user;
    setCSRF(data.csrf_token);
    if (!user.value.must_change_password)
      settings.value = await api<Settings>("/settings");
  }
  async function logout() {
    await api("/logout", "POST");
    user.value = null;
    setCSRF("");
  }
  async function preferences(values: Record<string, unknown>) {
    const merged = { ...user.value?.preferences, ...values };
    await api("/preferences", "PUT", merged);
    if (user.value) user.value.preferences = merged;
  }
  return { user, ready, settings, can, load, login, logout, preferences };
});
