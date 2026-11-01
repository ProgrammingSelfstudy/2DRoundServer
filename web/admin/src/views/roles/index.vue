<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>角色管理</h1>
      <el-button :loading="loading" @click="reload">刷新列表</el-button>
    </div>
    <form class="role-search" @submit.prevent="search">
      <el-input v-model="keyword" aria-label="角色搜索" placeholder="角色名、账号名或角色 ID" clearable />
      <el-select v-model="server" aria-label="角色区服筛选" clearable placeholder="全部区服" @change="search">
        <el-option label="全部区服" value="" /><el-option
          v-for="s in servers"
          :key="s.srv_id"
          :label="`${s.name}（${s.srv_id}）`"
          :value="s.srv_id"
        />
      </el-select>
      <el-button native-type="submit" type="primary" :loading="loading">搜索</el-button>
    </form>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="serverError" :title="serverError" type="warning" :closable="false" />
    <el-table :data="rows" v-loading="loading" row-key="rid" border>
      <el-table-column prop="rid" label="角色 ID" width="100" />
      <el-table-column prop="name" label="名称" min-width="140" /><el-table-column prop="account" label="账号" min-width="140" />
      <el-table-column prop="srv_id" label="区服" min-width="115" /><el-table-column prop="lev" label="等级" width="80" />
      <el-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><el-tag :type="row.status === 1 ? 'success' : 'danger'">{{ row.status === 1 ? "正常" : "已封禁" }}</el-tag></template
        ></el-table-column
      >
      <el-table-column label="创建时间" min-width="170"
        ><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column
      >
      <el-table-column label="最近登录" min-width="170"
        ><template #default="{ row }">{{ formatTime(row.last_login_at) }}</template></el-table-column
      >
      <el-table-column label="操作" :width="auth.hasPermission('write') ? 240 : 90" fixed="right"
        ><template #default="{ row }">
          <el-button link type="primary" @click="openDetail(row.rid)">详情</el-button>
          <template v-if="auth.hasPermission('write')">
            <el-button link :type="row.status === 1 ? 'danger' : 'success'" :disabled="busy" @click="setBan(row)">{{
              row.status === 1 ? "封禁" : "解封"
            }}</el-button>
            <el-button
              link
              type="primary"
              @click="router.push({ path: '/mail', query: { to: String(row.rid), srv_id: row.srv_id } })"
              >发邮件</el-button
            >
          </template>
        </template></el-table-column
      >
      <template #empty>暂无符合条件的角色</template>
    </el-table>
    <el-pagination
      :current-page="page"
      :page-size="limit"
      :page-sizes="[25, 50, 100]"
      :total="total"
      layout="total, sizes, prev, pager, next"
      @current-change="navigate($event, limit)"
      @size-change="navigate(1, $event)"
    />
    <p class="hint">列表使用服务端分页；发邮件会进入新版邮件页并带入角色和区服。</p>
    <RoleDialog v-if="selected !== null" :key="selected" :rid="selected" @close="selected = null" @changed="reload" />
  </div>
</template>
<script setup lang="ts" name="roles">
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getRoles, getServers, type RoleRow, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { confirmAction, useMutation } from "@/composables/useMutation";
import { errorMessage, formatTime } from "@/utils/admin";
const RoleDialog = defineAsyncComponent(() => import("./RoleDialog.vue"));
const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const { busy, run } = useMutation();
const keyword = ref("");
const server = ref("");
const page = ref(1);
const limit = ref(25);
const total = ref(0);
const rows = ref<RoleRow[]>([]);
const servers = ref<ServerRow[]>([]);
const loading = ref(false);
const error = ref("");
const serverError = ref("");
const selected = ref<number | null>(null);
let sequence = 0;
async function reload() {
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const result = await getRoles(
      String(route.query.q || ""),
      String(route.query.srv_id || ""),
      limit.value,
      (page.value - 1) * limit.value
    );
    if (seq !== sequence) return;
    rows.value = result.items;
    total.value = result.total;
  } catch (e) {
    if (seq === sequence) {
      rows.value = [];
      total.value = 0;
      error.value = errorMessage(e);
    }
  } finally {
    if (seq === sequence) loading.value = false;
  }
}
function navigate(nextPage = 1, nextLimit = limit.value) {
  void router.push({
    path: "/roles",
    query: {
      q: keyword.value.trim() || undefined,
      srv_id: server.value || undefined,
      page: String(nextPage),
      limit: String(nextLimit)
    }
  });
}
function search() {
  const same =
    keyword.value.trim() === String(route.query.q || "") && server.value === String(route.query.srv_id || "") && page.value === 1;
  if (same) void reload();
  else navigate(1);
}
function openDetail(rid: number) {
  selected.value = rid;
}
async function setBan(row: RoleRow) {
  const ban = row.status === 1;
  if (!(await confirmAction(`${ban ? "封禁" : "解封"}角色“${row.name}”（${row.rid}）？${ban ? "服务端将尝试踢下线。" : ""}`)))
    return;
  await run(async () => {
    await api(`/roles/${row.rid}/${ban ? "ban" : "unban"}`, { method: "POST", body: {} });
    ElMessage({ type: ban ? "warning" : "success", message: ban ? "封禁已保存；在线踢线结果需在网关确认" : "角色已解封" });
    await reload();
  });
}
watch(
  () => route.query,
  () => {
    if (route.name !== "roles") return;
    keyword.value = String(route.query.q || "");
    server.value = String(route.query.srv_id || "");
    const p = Number(route.query.page);
    page.value = Number.isSafeInteger(p) && p > 0 ? p : 1;
    const size = Number(route.query.limit);
    limit.value = [25, 50, 100].includes(size) ? size : 25;
    void reload();
  },
  { immediate: true }
);
onMounted(async () => {
  try {
    servers.value = (await getServers()).items;
  } catch (e) {
    serverError.value = `区服筛选加载失败：${errorMessage(e)}`;
  }
});
onBeforeUnmount(() => {
  sequence++;
});
</script>
<style scoped lang="scss">
.role-search {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}
.role-search .el-input {
  width: 280px;
}
.role-search .el-select {
  width: 260px;
}
</style>
