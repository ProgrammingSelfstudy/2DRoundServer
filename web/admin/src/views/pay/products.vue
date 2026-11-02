<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>商品列表</h1>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <p class="hint">
      服务端现在能发货的商品：钻石充值档位（游戏“充值”页签）、快乐币面板（主城“+”→“礼包展示”）和已接上发货的礼包与活动商品。不在这里的商品，玩家点了开不了单。
    </p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-tabs v-model="kind">
      <el-tab-pane v-for="k in kinds" :key="k" :label="`${k}（${count(k)}）`" :name="k" />
    </el-tabs>
    <el-table :data="shown" v-loading="loading" border max-height="620">
      <el-table-column prop="id" label="商品编号" width="110" />
      <el-table-column prop="name" label="名称" min-width="220" />
      <el-table-column label="价格" width="120"
        ><template #default="{ row }">¥{{ row.yuan }}</template></el-table-column
      >
      <el-table-column label="类别" width="150"
        ><template #default="{ row }">{{ productKind(row.id) }}</template></el-table-column
      >
      <template #empty>这一类还没有可发货的商品</template>
    </el-table>
  </div>
</template>
<script setup lang="ts" name="pay-products">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { api } from "@/auth/runtime";
import type { Product } from "@/domain/operations";
import { productKind } from "@/domain/payPages";
import { errorMessage } from "@/utils/admin";
const kinds = ["钻石充值档位", "快乐币面板", "礼包与活动商品"];
const kind = ref(kinds[0]);
const products = ref<Product[]>([]);
const loading = ref(false);
const error = ref("");
let active = true;
const count = (k: string) => products.value.filter(p => productKind(p.id) === k).length;
const shown = computed(() => products.value.filter(p => productKind(p.id) === kind.value));
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await api<{ items: Product[] }>("/pay/products");
    if (active) products.value = data.items;
  } catch (e) {
    if (active) error.value = `商品加载失败：${errorMessage(e)}`;
  } finally {
    if (active) loading.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => (active = false));
</script>
