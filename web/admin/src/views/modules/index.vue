<template>
  <CollectionPage
    :title="`模块总表 · ${categoryTitle(category)}`"
    :loading="loading"
    :error="error"
    hint="展示这一类的运行时注册覆盖；有接口不代表所有前端业务和写操作已开放。动态前缀表示模块族。"
    @refresh="load()"
  >
    <template #filters
      ><el-input v-model="q" placeholder="搜索中文名、键、窗口" aria-label="模块搜索" style="width: 320px" /><el-select
        v-model="scope"
        aria-label="模块范围"
        style="width: 160px"
        ><el-option label="全部范围" value="" /><el-option label="玩家" value="role" /><el-option
          label="全服"
          value="global" /></el-select
    ></template>
    <p>
      注册 {{ rows.length }} 项：玩家 {{ rows.filter(m => m.scope === "role").length }} / 全服
      {{ rows.filter(m => m.scope === "global").length }}；均可查看原始数据。
    </p>
    <RecordTable :rows="filtered" :columns="columns" :loading="loading" />
  </CollectionPage>
</template>
<script setup lang="ts" name="modules">
import { ref, computed, onMounted } from "vue";
import { useRoute } from "vue-router";
import { categoryOf, categoryTitle } from "@/domain/moduleCategories";
import { useCollection } from "@/composables/useCollection";
import type { ModuleInfo } from "@/domain/gameAdmin";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const route = useRoute();
// One list for each kind of module: the route (meta.category) says which.
const category = computed(() => String(route.meta.category || "other"));
const { rows: all, loading, error, load } = useCollection<ModuleInfo>("/modules", 200, false),
  q = ref(""),
  scope = ref("");
const rows = computed(() => all.value.filter(m => categoryOf(m.key) === category.value));
const filtered = computed(() =>
  rows.value.filter(
    m =>
      (!scope.value || m.scope === scope.value) && `${m.title} ${m.key} ${m.window}`.toLowerCase().includes(q.value.toLowerCase())
  )
);
const columns: Column[] = [
  { key: "scope", label: "范围", format: v => (v === "role" ? "玩家" : "全服") },
  { key: "key", label: "模块键", width: 220 },
  { key: "title", label: "中文名", width: 200 },
  { key: "window", label: "游戏窗口", width: 200 },
  { key: "has_view", label: "计算视图", format: v => (v ? "已接" : "原始JSON") },
  { key: "has_rank", label: "排名", format: v => (v ? "已接" : "不提供") },
  {
    key: "resets",
    label: "白名单重置项",
    width: 240,
    format: v => v.map((r: { item: string }) => r.item).join("、") || "不提供"
  },
  { key: "editable", label: "任意编辑", format: v => (v ? "已开放" : "不提供") }
];
onMounted(() => void load());
</script>
