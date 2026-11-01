<template>
  <CollectionPage
    title="举报记录"
    :loading="loading"
    :error="error"
    hint="最近 200 条玩家聊天举报，只读。服务端未保存客户端附带的聊天记录；如需封禁或踢线，请进入角色页核对。"
    @refresh="load"
    ><RecordTable :rows="rows" :columns="columns" :loading="loading"
      ><template #target_rid="{ row }"
        ><el-button link type="primary" @click="router.push({ path: '/roles', query: { q: String(row.target_rid) } })">{{
          row.target_rid
        }}</el-button></template
      ><template #actions="{ row }"><el-button @click="detail = row">查看内容</el-button></template></RecordTable
    ><el-dialog :model-value="!!detail" title="举报详情" @close="detail = undefined"
      ><p v-if="detail" style="white-space: pre-wrap">{{ detail.msg }}</p></el-dialog
    ></CollectionPage
  >
</template>
<script setup lang="ts" name="reports">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useCollection } from "@/composables/useCollection";
import type { Report } from "@/domain/management";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const router = useRouter(),
  detail = ref<Report>();
const { rows, loading, error, load } = useCollection<Report>("/reports", 200, false);
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "rid", label: "举报人" },
  { key: "target_rid", label: "被举报人" },
  { key: "target_srv_id", label: "区服" },
  { key: "type", label: "类型" },
  { key: "msg", label: "内容", width: 300 },
  { key: "created_at", label: "时间", time: true }
];
onMounted(() => load());
</script>
