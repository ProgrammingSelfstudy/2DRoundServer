<template>
  <CollectionPage
    title="管理员"
    :loading="loading"
    :error="error || commandError"
    hint="只能由超级管理员管理。不能降低或停用自己；服务端保护至少保留一名启用的超级管理员。角色变更会更新目标已有会话的权限；停用期间拒绝访问；密码重置使旧凭证失效。"
    @refresh="load"
    ><template #actions
      ><el-button v-if="auth.hasPermission('manage')" type="primary" :disabled="busy || uncertain" @click="create"
        >新建管理员</el-button
      ></template
    ><el-alert
      v-if="uncertain"
      title="提交结果不明，请先刷新核对管理员列表；密码重置需用新密码验证，勿连续重复提交。"
      type="warning"
      :closable="false" /><el-button v-if="uncertain" @click="review">已核对结果，继续操作</el-button
    ><RecordTable :rows="rows" :columns="columns" :loading="loading" :action-width="325"
      ><template #username="{ row }">{{ row.username }}{{ row.id === auth.admin?.id ? "（我）" : "" }}</template
      ><template #actions="{ row }"
        ><template v-if="auth.hasPermission('manage')"
          ><el-button :disabled="busy || uncertain || row.id === auth.admin?.id" @click="editRole(row)">调整角色</el-button
          ><el-button
            :disabled="busy || uncertain || row.id === auth.admin?.id"
            :type="row.status === 1 ? 'danger' : 'primary'"
            @click="toggle(row)"
            >{{ row.status === 1 ? "禁用" : "启用" }}</el-button
          ><el-button :disabled="busy || uncertain" @click="reset(row)">重置密码</el-button></template
        ></template
      ></RecordTable
    ><el-dialog
      v-model="dialog"
      :title="
        mode === 'create'
          ? '新建管理员'
          : mode === 'role'
            ? `调整 ${selected?.username} 的角色`
            : `重置 ${selected?.username} 的密码`
      "
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      ><el-form label-position="top" :disabled="busy || uncertain" @submit.prevent="save"
        ><el-form-item v-if="mode === 'create'" label="用户名"
          ><el-input
            v-model="form.username"
            maxlength="32"
            autocomplete="off"
            aria-label="管理员用户名"
            placeholder="3～32 位字母、数字、_ . -" /></el-form-item
        ><el-form-item v-if="mode !== 'role'" label="新密码（8～72 字节）"
          ><el-input
            v-model="form.password"
            type="password"
            autocomplete="new-password"
            aria-label="管理员新密码" /></el-form-item
        ><el-form-item v-if="mode !== 'reset'" label="角色"
          ><el-select v-model="form.role" aria-label="管理员角色"
            ><el-option v-for="(label, role) in roleLabels" :key="role" :value="role" :label="label" /></el-select
        ></el-form-item>
        <p v-if="mode === 'reset'">重置后目标所有登录会话失效。重置自己的密码后会退出并重新登录。</p>
        <el-button @click="close">取消</el-button
        ><el-button type="primary" :loading="busy" @click="save">{{
          mode === "create" ? "创建" : mode === "role" ? "保存角色" : "确认重置"
        }}</el-button></el-form
      ><el-alert v-if="commandError" :title="commandError" type="error" :closable="false" /></el-dialog
  ></CollectionPage>
</template>
<script setup lang="ts" name="admins">
import { onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { api, session } from "@/auth/runtime";
import { roleLabels, type Role } from "@/auth/permissions";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { adminPayload, password, type Admin } from "@/domain/management";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const auth = useAuthStore(),
  router = useRouter(),
  dialog = ref(false),
  mode = ref<"create" | "role" | "reset">("create"),
  selected = ref<Admin>(),
  form = ref({ username: "", password: "", role: "operator" as Role });
const { rows, loading, error, load } = useCollection<Admin>("/admins", 200, false);
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("manage", true);
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "username", label: "用户名", width: 190 },
  { key: "role", label: "角色", format: v => roleLabels[v as Role] || v },
  { key: "status", label: "状态", format: v => (v === 1 ? "启用" : "已禁用") },
  { key: "last_login_at", label: "最近登录", time: true },
  { key: "created_at", label: "创建时间", time: true }
];
function create() {
  mode.value = "create";
  selected.value = undefined;
  form.value = { username: "", password: "", role: "operator" };
  dialog.value = true;
}
function editRole(a: Admin) {
  mode.value = "role";
  selected.value = a;
  form.value = { username: a.username, password: "", role: a.role };
  dialog.value = true;
}
function reset(a: Admin) {
  mode.value = "reset";
  selected.value = a;
  form.value = { username: a.username, password: "", role: a.role };
  dialog.value = true;
}
function close() {
  dialog.value = false;
  form.value.password = "";
}
watch(dialog, v => {
  if (!v) form.value.password = "";
});
function relogin() {
  session.clear();
  auth.loginNotice = "密码已修改，请用新密码重新登录。";
  void router.replace("/login");
}
async function save() {
  const r = await execute(
    () =>
      api<Admin>(mode.value === "create" ? "/admins" : `/admins/${selected.value!.id}`, {
        method: mode.value === "create" ? "POST" : "PATCH",
        body:
          mode.value === "create"
            ? adminPayload(form.value.username, form.value.password, form.value.role)
            : mode.value === "role"
              ? { role: form.value.role }
              : { password: password(form.value.password) }
      }),
    mode.value === "reset"
      ? "重置密码并使目标所有会话失效？"
      : mode.value === "role"
        ? "保存角色并更新目标已有会话权限？"
        : "创建此管理员？"
  );
  if (r) {
    const own = mode.value === "reset" && selected.value?.id === auth.admin?.id;
    close();
    if (own) {
      relogin();
      return;
    }
    ElMessage.success("管理员已更新");
    void load();
  }
}
async function toggle(a: Admin) {
  const r = await execute(
    () => api(`/admins/${a.id}`, { method: "PATCH", body: { status: a.status === 1 ? 0 : 1 } }),
    `${a.status === 1 ? "禁用" : "启用"}管理员 ${a.username}？${a.status === 1 ? "停用期间已有会话无法访问。" : "允许按当前角色访问；未过期且未改密的旧凭证也可能恢复使用。"}`
  );
  if (r) {
    ElMessage.success("状态已更新");
    void load();
  }
}
async function review() {
  if (await load()) reviewed();
}
onMounted(() => load());
</script>
