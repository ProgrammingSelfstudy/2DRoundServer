<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>手工补单</h1>
      <el-button @click="router.push('/pay/orders')">去订单管理</el-button>
    </div>
    <el-alert
      title="玩家已线下付款时使用"
      description="补单只用于线下已收到款的情况。勾选“立即确认到账”会马上发货，发货后不能撤回。"
      type="info"
      :closable="false"
    />
    <el-alert v-if="productError" :title="productError" type="error" :closable="false" />
    <el-card shadow="never" class="manual">
      <el-form label-position="top" :disabled="manualBusy || manualUncertain" @submit.prevent="createOrder">
        <el-form-item label="查找充值角色"><RoleLookup label="查找充值角色" @select="selectRole" /></el-form-item>
        <p v-if="roleLabel" class="hint">已选择：{{ roleLabel }}</p>
        <div class="form-grid">
          <el-form-item label="角色 ID" label-for="pay-rid"
            ><IntegerInput id="pay-rid" v-model="rid" :min="1" :precision="0" @change="roleLabel = ''"
          /></el-form-item>
          <el-form-item label="可发货商品" label-for="pay-product"
            ><el-select id="pay-product" v-model="charge" filterable
              ><el-option
                v-for="p in products"
                :key="p.id"
                :value="p.id"
                :label="`${p.name}（¥${p.yuan}，编号 ${p.id}）`" /></el-select
          ></el-form-item>
        </div>
        <el-form-item label="补单备注" label-for="pay-note"
          ><el-input id="pay-note" v-model="note" maxlength="100" placeholder="线下收款核对信息"
        /></el-form-item>
        <el-checkbox v-model="confirmNow">立即确认到账并发货</el-checkbox>
        <p class="hint">未勾选时只创建等待付款订单，需在订单管理单独确认到账；此时服务端不保存补单备注。发货后不可通过取消撤回。</p>
        <el-alert
          v-if="formError || manualCommand.error.value"
          :title="formError || manualCommand.error.value"
          type="error"
          :closable="false"
        />
        <el-button type="primary" native-type="submit" :loading="manualBusy" :disabled="!productsReady || !products.length"
          >创建补单</el-button
        >
      </el-form>
      <el-alert
        v-if="manualUncertain"
        title="补单结果未确认，请勿重复提交"
        description="订单或发货可能已经写入。请先到订单管理核对，再决定是否新建另一笔。"
        type="warning"
        :closable="false"
        ><el-button @click="reviewManual">已核对订单，重新填写</el-button></el-alert
      >
    </el-card>
    <el-alert v-if="result" :title="result.message" :type="result.type" :closable="false" show-icon />
  </div>
</template>
<script setup lang="ts" name="pay-manual">
import IntegerInput from "@/components/Admin/IntegerInput.vue";
import RoleLookup from "@/components/Admin/RoleLookup.vue";
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api } from "@/auth/runtime";
import type { RoleRow } from "@/api/admin";
import { useCommand } from "@/composables/useCommand";
import { manualOrder, paymentFeedback, type OrderReply, type Product } from "@/domain/operations";
import { errorMessage } from "@/utils/admin";
import { refreshPendingPay } from "@/composables/usePendingPay";
const router = useRouter();
const manualCommand = useCommand("manage", true);
const { busy: manualBusy, uncertain: manualUncertain } = manualCommand;
const products = ref<Product[]>([]);
const productsReady = ref(false);
const productError = ref("");
const rid = ref<number>();
const charge = ref<number>();
const note = ref("");
const confirmNow = ref(true);
const roleLabel = ref("");
const formError = ref("");
const result = ref<{ type: "success" | "warning"; message: string }>();
let active = true;
async function loadProducts() {
  productError.value = "";
  try {
    const data = await api<{ items: Product[] }>("/pay/products");
    if (active) {
      products.value = data.items;
      productsReady.value = true;
      charge.value ??= data.items[0]?.id;
    }
  } catch (e) {
    if (active) productError.value = `可发货商品加载失败：${errorMessage(e)}`;
  }
}
function selectRole(role: RoleRow) {
  rid.value = role.rid;
  roleLabel.value = `${role.name}（${role.rid} / ${role.srv_id}，账号 ${role.account}）`;
}
function resetForm() {
  rid.value = undefined;
  roleLabel.value = "";
  note.value = "";
}
async function createOrder() {
  formError.value = "";
  let body: ReturnType<typeof manualOrder>;
  try {
    body = manualOrder(rid.value, charge.value, confirmNow.value, note.value, products.value);
  } catch (e) {
    formError.value = errorMessage(e);
    return;
  }
  const product = products.value.find(p => p.id === body.charge_id)!;
  const saved = await manualCommand.execute(
    () => api<OrderReply>("/pay/orders", { method: "POST", body }),
    `为角色 ${body.rid} 创建 ${product.name}（¥${product.yuan}）补单，${body.confirm ? "立即确认到账并发货" : "仅创建等待付款订单，不发货"}？`
  );
  if (saved && active) {
    result.value = paymentFeedback(saved);
    resetForm();
    void refreshPendingPay();
  }
}
function reviewManual() {
  manualCommand.reviewed();
  resetForm();
}
onMounted(loadProducts);
onBeforeUnmount(() => (active = false));
</script>
<style scoped lang="scss">
.manual {
  margin: 24px 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}
@media (max-width: 650px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
