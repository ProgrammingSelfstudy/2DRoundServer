<template>
  <CollectionPage
    title="审计日志"
    :loading="loading"
    :error="error"
    paged
    :limit="30"
    :page="page"
    :total="total"
    hint="仅超级管理员可查看，按接口返回顺序分页。"
    @refresh="load"
    @page="changePage"
    ><RecordTable :rows="rows" :columns="columns" :loading="loading"
      ><template #actions="{ row }"><el-button @click="detail = row">查看详情</el-button></template></RecordTable
    ><el-dialog :model-value="!!detail" title="操作详情" @close="detail = undefined"
      ><p>{{ detail?.admin_name }} · {{ detail?.action }} · {{ detail?.target }}</p>
      <pre style="white-space: pre-wrap; overflow-wrap: anywhere">{{ detail?.detail }}</pre>
    </el-dialog></CollectionPage
  >
</template>
<script setup lang="ts" name="audit">
import { onMounted, ref } from "vue";
import { useCollection } from "@/composables/useCollection";
import type { Audit } from "@/domain/management";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const detail = ref<Audit>();
const { rows, loading, error, page, total, load } = useCollection<Audit>("/audit", 30);
const columns: Column[] = [
  { key: "created_at", label: "时间", time: true, width: 180 },
  { key: "admin_name", label: "管理员" },
  { key: "action", label: "操作", width: 180 },
  { key: "target", label: "对象" },
  { key: "ip", label: "IP" },
  { key: "detail", label: "详情", width: 300 }
];
function changePage(v: number) {
  page.value = v;
  void load();
}
onMounted(() => load());
</script>
