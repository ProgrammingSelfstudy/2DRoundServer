<template>
  <main class="login-page">
    <section class="login-card">
      <h1>2DRound 管理后台</h1>
      <p>使用本项目的管理员账号登录</p>
      <el-alert v-if="error || auth.loginNotice" :title="error || auth.loginNotice" type="warning" :closable="false" show-icon />
      <form @submit.prevent="submit">
        <label for="admin-username">用户名</label>
        <el-input
          id="admin-username"
          v-model="username"
          name="username"
          autocomplete="username"
          :disabled="loading"
          required
          autofocus
          maxlength="32"
        />
        <label for="admin-password">密码</label>
        <el-input
          id="admin-password"
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          :disabled="loading"
          required
        />
        <el-button type="primary" native-type="submit" :loading="loading" :disabled="remaining > 0">{{
          remaining ? `${remaining} 秒后重试` : "登录"
        }}</el-button>
      </form>
    </section>
  </main>
</template>
<script setup lang="ts" name="login">
import { onBeforeUnmount, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAuthStore } from "@/stores/modules/auth";
import { ApiError } from "@/api/client";
import { errorMessage } from "@/utils/admin";
const username = ref("");
const password = ref("");
const loading = ref(false);
const error = ref("");
const remaining = ref(0);
let timer: ReturnType<typeof setInterval> | undefined;
const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
async function submit() {
  if (loading.value || remaining.value) return;
  loading.value = true;
  error.value = "";
  auth.loginNotice = "";
  try {
    await auth.login(username.value.trim(), password.value);
    const redirect = typeof route.query.redirect === "string" ? route.query.redirect : "";
    // Resolve only local router paths. Never redirect credentials to an external site.
    const next =
      redirect.startsWith("/") && !redirect.startsWith("//") && !redirect.includes("\\") && !redirect.startsWith("/login")
        ? redirect
        : "/dashboard";
    await router.replace(next);
  } catch (e) {
    error.value = errorMessage(e);
    if (e instanceof ApiError && e.status === 429) {
      remaining.value = Math.ceil(e.retryAfter || 60);
      clearInterval(timer);
      timer = setInterval(() => {
        if (--remaining.value <= 0) clearInterval(timer);
      }, 1000);
    }
  } finally {
    password.value = "";
    loading.value = false;
  }
}
onBeforeUnmount(() => clearInterval(timer));
</script>
<style scoped lang="scss">
.login-page {
  display: grid;
  min-height: 100vh;
  place-items: center;
  background: var(--el-fill-color-light);
}
.login-card {
  width: min(380px, calc(100vw - 64px));
  padding: 32px;
  background: var(--el-bg-color);
  border-radius: 12px;
  box-shadow: var(--el-box-shadow-light);
}
h1 {
  margin-top: 0;
  font-size: 24px;
}
p {
  color: var(--el-text-color-secondary);
}
form {
  margin: 24px 0;
}
label {
  display: block;
  margin: 16px 0 8px;
}
.el-button {
  width: 100%;
  margin-top: 24px;
}
</style>
