<template>
  <div v-loading="loading">
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-select v-model="selected" filterable aria-label="数据表" style="width: min(100%, 460px)" @change="changeTable">
      <el-option
        v-for="t in options"
        :key="t.name"
        :value="t.name"
        :label="`${t.group} / ${t.title}（${t.name}）${counts[t.name] == null ? '' : ' · ' + counts[t.name] + '条'}`"
      />
    </el-select>
    <template v-if="table">
      <p>{{ table.title }} · 归属：{{ table.owner }} · 只读；文本最多 4KB，截断列见标记。</p>
      <el-button :disabled="loading" @click="load">刷新数据</el-button>
      <el-table :data="rows" border max-height="500">
        <el-table-column v-for="c in table.columns" :key="c" :label="c" :min-width="160"
          ><template #default="{ row }"
            ><span style="white-space: pre-wrap; overflow-wrap: anywhere">{{ row[c] ?? "—" }}</span></template
          ></el-table-column
        >
        <el-table-column label="截断标记" min-width="130"
          ><template #default="{ row }">{{ (row.truncated || []).join("、") || "无" }}</template></el-table-column
        >
      </el-table>
      <el-pagination
        :current-page="page"
        :page-size="50"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="changePage"
      />
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { api } from "@/auth/runtime";
import { tablePath, type TableInfo } from "@/domain/gameAdmin";
import { errorMessage } from "@/utils/admin";
const props = defineProps<{ scope: TableInfo["scope"]; owner: string | number; group?: string }>();
const options = ref<TableInfo[]>([]),
  selected = ref(""),
  rows = ref<Record<string, any>[]>([]),
  counts = ref<Record<string, number>>({}),
  total = ref(0),
  page = ref(1),
  loading = ref(false),
  error = ref("");
const table = computed(() => options.value.find(t => t.name === selected.value));
let seq = 0,
  active = true;
async function load() {
  const token = ++seq;
  rows.value = [];
  // total stays while a page loads: with total 0 the pager has one page and answers the current page 2 with
  // current-change(1), which would load page 1 again and make every page after the first unreachable
  error.value = "";
  loading.value = true;
  try {
    if (!table.value) return;
    const r = await api<{ rows: Record<string, any>[]; total: number }>(tablePath(table.value, props.owner, page.value));
    if (active && token === seq) {
      rows.value = r.rows;
      total.value = r.total;
    }
  } catch (e) {
    if (active && token === seq) error.value = errorMessage(e);
  } finally {
    if (active && token === seq) loading.value = false;
  }
}
function changePage(n: number) {
  page.value = n;
  void load();
}
function changeTable() {
  page.value = 1;
  total.value = 0;
  void load();
}
watch(
  () => [props.scope, props.owner, props.group],
  async () => {
    const token = ++seq;
    options.value = [];
    rows.value = [];
    total.value = 0;
    page.value = 1; // the same instance serves every group tab, and a page left over from another table is out of range here
    counts.value = {};
    selected.value = "";
    error.value = "";
    loading.value = true;
    try {
      const [catalog, summary] = await Promise.all([
        api<{ items: TableInfo[] }>("/tables"),
        props.scope === "role"
          ? api<{ items: { name: string; total: number }[] }>(`/roles/${encodeURIComponent(props.owner)}/tables`)
          : Promise.resolve({ items: [] })
      ]);
      if (!active || token !== seq) return;
      options.value = catalog.items.filter(t => t.scope === props.scope && (!props.group || t.group === props.group));
      counts.value = Object.fromEntries(summary.items.map(t => [t.name, t.total]));
      selected.value = options.value[0]?.name || "";
      await load();
    } catch (e) {
      if (active && token === seq) error.value = errorMessage(e);
    } finally {
      if (active && token === seq) loading.value = false;
    }
  },
  { immediate: true }
);
onBeforeUnmount(() => {
  active = false;
  seq++;
});
</script>
