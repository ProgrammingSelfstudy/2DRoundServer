<template>
  <CollectionPage
    title="游戏账号"
    :loading="loading"
    :error="error || commandError"
    paged
    :page="page"
    :total="total"
    hint="这是玩家登录账号；后台人员请到管理员页管理。封禁使账号登录凭证失效，服务端同时尝试踢出名下角色。当前接口不返回踢线结果。"
    @refresh="loadRows"
    @page="changePage"
    ><template #filters
      ><el-form inline @submit.prevent="search"
        ><el-form-item><el-input v-model="q" aria-label="账号搜索" placeholder="账号名或 ID" clearable /></el-form-item
        ><el-button @click="search">搜索</el-button></el-form
      ></template
    ><RecordTable :rows="rows" :columns="columns" :loading="loading"
      ><template #role_count="{ row }"
        ><el-button link type="primary" @click="router.push({ path: '/roles', query: { q: row.account } })"
          >{{ row.role_count }} 个角色</el-button
        ></template
      ><template #actions="{ row }"
        ><el-button @click="detail = row">详情</el-button
        ><el-button
          v-if="auth.hasPermission('write')"
          :disabled="busy"
          :type="row.status === 1 ? 'danger' : 'primary'"
          @click="operate(row)"
          >{{ row.status === 1 ? "封禁" : "解封" }}</el-button
        ></template
      ></RecordTable
    ><el-dialog :model-value="!!detail" title="账号详情" @close="detail = undefined"
      ><el-descriptions v-if="detail" :column="1" border
        ><el-descriptions-item v-for="c in columns" :key="c.key" :label="c.label">{{
          c.time ? formatTime((detail as any)[c.key]) : (detail as any)[c.key]
        }}</el-descriptions-item></el-descriptions
      ></el-dialog
    ><el-dialog
      v-model="dialog"
      title="封禁游戏账号"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      ><p>封禁 {{ selected?.account }}，名下角色会尝试踢线。</p>
      <el-form :disabled="busy" @submit.prevent="ban"
        ><el-form-item label="原因"
          ><el-input
            v-model="reason"
            maxlength="200"
            show-word-limit
            aria-label="封禁原因"
            placeholder="玩家不可见，可留空" /></el-form-item
        ><el-button @click="dialog = false">取消</el-button
        ><el-button type="danger" :loading="busy" @click="ban">确认封禁</el-button></el-form
      ></el-dialog
    ></CollectionPage
  >
</template>
<script setup lang="ts" name="accounts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { useCollection } from "@/composables/useCollection";
import { banReason, type Account } from "@/domain/management";
import { formatTime } from "@/utils/admin";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const router = useRouter(),
  auth = useAuthStore(),
  q = ref(String(useRoute().query.q || "")),
  filter = ref(q.value),
  detail = ref<Account>(),
  selected = ref<Account>(),
  reason = ref(""),
  dialog = ref(false);
const { rows, loading, error, page, total, load } = useCollection<Account>("/accounts");
const { busy, error: commandError, execute } = useCommand("write");
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "platform", label: "平台" },
  { key: "account", label: "账号" },
  { key: "role_count", label: "角色数" },
  { key: "status", label: "状态", format: v => (v === 1 ? "正常" : "已封禁") },
  { key: "ban_reason", label: "封禁原因" },
  { key: "created_at", label: "注册时间", time: true },
  { key: "last_login_at", label: "最近登录", time: true }
];
const loadRows = () => load({ q: filter.value });
function search() {
  filter.value = q.value.trim();
  page.value = 1;
  void loadRows();
}
function changePage(v: number) {
  page.value = v;
  void loadRows();
}
async function operate(a: Account) {
  if (a.status === 1) {
    selected.value = a;
    reason.value = "";
    dialog.value = true;
    return;
  }
  const r = await execute(() => api(`/accounts/${a.id}/unban`, { method: "POST" }), `解封账号 ${a.account}？`);
  if (r) {
    ElMessage.success("已解封");
    void loadRows();
  }
}
async function ban() {
  if (!selected.value) return;
  const r = await execute(() => api(`/accounts/${selected.value!.id}/ban`, { method: "POST", body: banReason(reason.value) }));
  if (r) {
    dialog.value = false;
    ElMessage.success("账号已封禁；踢线结果请另行核对");
    void loadRows();
  }
}
onMounted(loadRows);
</script>
