<template>
  <CollectionPage
    title="公会管理"
    :loading="loading"
    :error="error || optionsError || commandError"
    paged
    :page="page"
    :total="total"
    hint="解散后成员、申请和日志会删除，会长需等待冷却；在线成员重新登录后才会发现。"
    @refresh="loadRows"
    @page="changePage"
    ><template #filters
      ><el-form inline @submit.prevent="search"
        ><el-form-item label="区服"
          ><el-select v-model="srv" :empty-values="[null, undefined]" aria-label="公会区服" style="width: 240px"
            ><el-option label="全部区服" value="" /><el-option
              v-for="s in servers"
              :key="s.srv_id"
              :value="s.srv_id"
              :label="`${s.name}（${s.srv_id}）`" /></el-select></el-form-item
        ><el-form-item><el-input v-model="q" placeholder="公会名称" aria-label="公会名称" clearable /></el-form-item
        ><el-button @click="search">搜索</el-button></el-form
      ></template
    ><RecordTable :rows="rows" :columns="columns" :loading="loading"
      ><template #actions="{ row }"
        ><el-button @click="detail = row">详情</el-button
        ><el-button @click="router.push(statusLocation(row.srv_id, row.gid))">数据表</el-button
        ><el-button v-if="auth.hasPermission('write')" type="danger" :disabled="busy" @click="disband(row)"
          >解散</el-button
        ></template
      ></RecordTable
    ><el-dialog :model-value="!!detail" title="公会详情" @close="detail = undefined"
      ><el-descriptions v-if="detail" :column="1" border
        ><el-descriptions-item v-for="c in columns" :key="c.key" :label="c.label">{{
          c.time ? formatTime((detail as any)[c.key]) : (detail as any)[c.key]
        }}</el-descriptions-item></el-descriptions
      ></el-dialog
    ></CollectionPage
  >
</template>
<script setup lang="ts" name="guilds">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getServers, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import type { Guild } from "@/domain/management";
import { statusLocation } from "@/domain/gameAdmin";
import { errorMessage, formatTime } from "@/utils/admin";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const router = useRouter(),
  auth = useAuthStore(),
  srv = ref(""),
  q = ref(""),
  filter = ref({ srv_id: "", q: "" }),
  servers = ref<ServerRow[]>([]),
  optionsError = ref(""),
  detail = ref<Guild>();
const { rows, loading, error, page, total, load } = useCollection<Guild>("/guilds");
const { busy, error: commandError, execute } = useCommand("write");
const columns: Column[] = [
  { key: "gid", label: "ID" },
  { key: "srv_id", label: "区服" },
  { key: "name", label: "名称" },
  { key: "lev", label: "等级" },
  { key: "members", label: "人数" },
  { key: "leader", label: "会长" },
  { key: "leader_rid", label: "会长编号" },
  { key: "created_at", label: "创建", time: true }
];
const loadRows = () => load(filter.value);
function search() {
  filter.value = { srv_id: srv.value, q: q.value.trim() };
  page.value = 1;
  void loadRows();
}
function changePage(v: number) {
  page.value = v;
  void loadRows();
}
async function disband(g: Guild) {
  const r = await execute(
    () => api(`/guilds/${g.gid}/disband`, { method: "POST", body: {} }),
    `解散公会“${g.name}”（${g.gid}）？成员、申请和日志都会删除，会长需等待冷却。`
  );
  if (r) {
    ElMessage.success("已解散；在线成员重新登录后生效");
    void loadRows();
  }
}
onMounted(() => {
  void loadRows();
  void getServers()
    .then(r => (servers.value = r.items))
    .catch(e => (optionsError.value = errorMessage(e)));
});
</script>
