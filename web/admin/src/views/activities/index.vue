<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>活动管理</h1>
      <div>
        <el-button :loading="loading" @click="load">刷新列表</el-button
        ><el-button v-if="auth.hasPermission('write')" type="primary" @click="open()">新建活动</el-button>
      </div>
    </div>
    <div class="filters">
      <el-select v-model="srv" :empty-values="[null, undefined]" placeholder="全部区服" aria-label="活动区服筛选" @change="filter"
        ><el-option label="全部区服" value="" /><el-option
          v-for="s in servers"
          :key="s.srv_id"
          :value="s.srv_id"
          :label="`${s.name}（${s.srv_id}）`" /></el-select
      ><span class="hint">选定区服时也包含全服活动。</span>
    </div>
    <el-alert
      v-if="error || optionsError || commandError"
      :title="error || optionsError || commandError"
      type="error"
      :closable="false"
    />
    <el-table :data="rows" v-loading="loading" border row-key="id">
      <el-table-column prop="id" label="ID" width="80" /><el-table-column label="区服" min-width="120"
        ><template #default="{ row }">{{ row.srv_id || "全服" }}</template></el-table-column
      >
      <el-table-column prop="camp_id" label="活动号" width="100" /><el-table-column
        prop="bid"
        label="页签"
        width="100"
      /><el-table-column prop="spec.title" label="标题" min-width="160" /><el-table-column
        prop="spec.panel_type"
        label="面板"
        width="85"
      />
      <el-table-column label="开始" min-width="175"
        ><template #default="{ row }">{{ formatTime(row.start_at) }}</template></el-table-column
      >
      <el-table-column label="结束" min-width="175"
        ><template #default="{ row }">{{ row.end_at ? formatTime(row.end_at) : "不结束" }}</template></el-table-column
      >
      <el-table-column label="状态" width="110"
        ><template #default="{ row }"
          ><el-tag
            :type="activityState(row, now) === '进行中' ? 'success' : activityState(row, now) === '未开始' ? 'warning' : 'info'"
            >{{ activityState(row, now) }}</el-tag
          ></template
        ></el-table-column
      >
      <el-table-column v-if="auth.hasPermission('write')" label="操作" width="195" fixed="right"
        ><template #default="{ row }"
          ><el-button link type="primary" :disabled="busy" @click="open(row)">编辑</el-button
          ><el-button link type="primary" :disabled="busy" @click="open(row, true)">复制</el-button
          ><el-button link type="danger" :disabled="busy" @click="remove(row)">删除</el-button></template
        ></el-table-column
      >
      <template #empty>暂无活动</template>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      :page-size="25"
      :total="total"
      layout="total, prev, pager, next"
      @current-change="load"
    />
    <ActivityDialog v-if="dialog" :row="selected" :copy="copy" :servers="servers" @close="closed" @saved="saved" />
  </div>
</template>
<script setup lang="ts" name="activities">
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getServers, query, type ServerRow } from "@/api/admin";
import { activityState, jsonClone, type Activity } from "@/domain/operations";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { errorMessage, formatTime } from "@/utils/admin";
const ActivityDialog = defineAsyncComponent(() => import("./ActivityDialog.vue"));
const auth = useAuthStore();
const { busy, error: commandError, execute } = useCommand("write");
const rows = ref<Activity[]>([]);
const servers = ref<ServerRow[]>([]);
const srv = ref("");
const page = ref(1);
const total = ref(0);
const loading = ref(false);
const error = ref("");
const optionsError = ref("");
const dialog = ref(false);
const selected = ref<Activity>();
const copy = ref(false);
const now = ref(Math.floor(Date.now() / 1000));
let sequence = 0;
let active = true;
const clock = setInterval(() => {
  if (document.visibilityState !== "hidden") now.value = Math.floor(Date.now() / 1000);
}, 30000);
async function load() {
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const data = await api<{ items: Activity[]; total: number }>(
      `/activities${query({ srv_id: srv.value, limit: 25, offset: (page.value - 1) * 25 })}`
    );
    if (active && seq === sequence) {
      rows.value = data.items;
      total.value = data.total;
      now.value = Math.floor(Date.now() / 1000);
    }
  } catch (e) {
    if (active && seq === sequence) {
      error.value = errorMessage(e);
      rows.value = [];
    }
  } finally {
    if (active && seq === sequence) loading.value = false;
  }
}
function filter() {
  page.value = 1;
  void load();
}
function open(row?: Activity, cloning = false) {
  selected.value = row ? jsonClone(row) : undefined;
  copy.value = cloning;
  dialog.value = true;
}
function closed() {
  dialog.value = false;
  void load();
}
function saved() {
  dialog.value = false;
  page.value = 1;
  void load();
}
async function remove(row: Activity) {
  const result = await execute(
    () => api(`/activities/${row.id}`, { method: "DELETE" }),
    `删除活动“${row.spec.title}”（${row.id}）？玩家已有的进度不会删除。`
  );
  if (result && active) {
    ElMessage.success("活动已删除");
    void load();
  }
}
onMounted(() => {
  void load();
  void getServers()
    .then(data => {
      if (active) servers.value = data.items;
    })
    .catch(e => {
      if (active) optionsError.value = `区服选项加载失败：${errorMessage(e)}`;
    });
});
onBeforeUnmount(() => {
  active = false;
  sequence++;
  clearInterval(clock);
});
</script>
<style scoped lang="scss">
.filters {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-bottom: 18px;
  flex-wrap: wrap;
}
.filters .el-select {
  width: 260px;
}
</style>
