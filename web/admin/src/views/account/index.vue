<template>
  <div class="management-page">
    <h1>我的账号</h1>
    <el-descriptions :column="1" border
      ><el-descriptions-item label="ID">{{ auth.admin?.id }}</el-descriptions-item
      ><el-descriptions-item label="用户名">{{ auth.admin?.username }}</el-descriptions-item
      ><el-descriptions-item label="角色">{{
        auth.admin ? roleLabels[auth.admin.role] : "—"
      }}</el-descriptions-item></el-descriptions
    >
    <p>修改密码会使所有已登录会话失效，包括当前会话。密码长度为 8 到 72 字节。</p>
    <el-form style="max-width: 560px" label-position="top" :disabled="busy || uncertain" @submit.prevent="save"
      ><el-form-item label="当前密码"
        ><el-input v-model="old" type="password" autocomplete="current-password" aria-label="当前密码" /></el-form-item
      ><el-form-item label="新密码"
        ><el-input v-model="next" type="password" autocomplete="new-password" aria-label="新密码" /></el-form-item
      ><el-form-item label="再次输入新密码"
        ><el-input v-model="repeat" type="password" autocomplete="new-password" aria-label="再次输入新密码" /></el-form-item
      ><el-button type="primary" :loading="busy" @click="save">修改密码</el-button></el-form
    ><el-alert v-if="error" :title="error" type="error" :closable="false" /><el-alert
      v-if="uncertain"
      title="改密结果不明，请退出后尝试用新密码登录，避免重复提交。"
      type="warning"
      :closable="false"
    /><el-button v-if="uncertain" @click="exit">退出并重新登录</el-button>
  </div>
</template>
<script setup lang="ts" name="account">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { api, session } from "@/auth/runtime";
import { roleLabels } from "@/auth/permissions";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { ownPassword } from "@/domain/management";
const auth = useAuthStore(),
  router = useRouter(),
  old = ref(""),
  next = ref(""),
  repeat = ref("");
const { busy, uncertain, error, execute } = useCommand("read", true);
function exit() {
  old.value = "";
  next.value = "";
  repeat.value = "";
  session.clear();
  auth.loginNotice = "请使用当前有效密码重新登录。";
  void router.replace("/login");
}
async function save() {
  const r = await execute(
    () => api("/me/password", { method: "POST", body: ownPassword(old.value, next.value, repeat.value) }),
    "修改密码并退出所有已登录会话？"
  );
  if (r) {
    exit();
    auth.loginNotice = "密码已修改，请用新密码重新登录。";
  }
}
</script>
