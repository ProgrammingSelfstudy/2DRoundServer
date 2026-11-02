<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>订单管理</h1>
      <el-button :loading="loading" @click="loadOrders">刷新订单</el-button>
    </div>
    <el-alert
      title="人工收款确认"
      description="请先核对实际收款，再确认到账发货。未收到款的订单不要确认；本页面不会接入或发起第三方支付。"
      type="info"
      :closable="false"
    />
    <div class="filters">
      <el-select
        v-model="status"
        :empty-values="[null, undefined]"
        placeholder="全部状态"
        aria-label="订单状态"
        @change="loadOrders"
        ><el-option label="等待付款" value="0" /><el-option label="已到账" value="1" /><el-option
          label="已取消"
          value="2" /><el-option label="全部状态" value=""
      /></el-select>
      <el-select v-model="limit" aria-label="订单条数" @change="loadOrders"
        ><el-option v-for="n in [25, 50, 100, 200]" :key="n" :value="n" :label="`最近 ${n} 笔`"
      /></el-select>
    </div>
    <p class="hint">
      当前显示所选状态最近 {{ limit }} 笔订单，可调整条数或状态。仅支持“商品列表”里的可发货商品，购买资格会在提交时校验。
    </p>
    <p v-if="!manage" class="hint">当前账号仅可查看订单；确认和取消仅限超级管理员。</p>
    <el-alert v-if="error || productError" :title="error || productError" type="error" :closable="false" />
    <el-alert v-if="orderCommand.error.value" :title="orderCommand.error.value" type="error" :closable="false" />
    <el-alert v-if="orderUncertain" title="处理结果未确认，请先核对订单状态，避免重复操作" type="warning" :closable="false"
      ><el-button @click="reviewOrder">已核对订单，解除操作锁定</el-button></el-alert
    >
    <el-table :data="orders" v-loading="loading" border row-key="order_no">
      <el-table-column prop="order_no" label="订单号" min-width="215" />
      <el-table-column label="角色 / 区服" min-width="180"
        ><template #default="{ row }"
          >{{ row.role || "未知角色" }}（{{ row.rid }}）<br />{{ row.srv_id }}</template
        ></el-table-column
      >
      <el-table-column label="商品" min-width="170"
        ><template #default="{ row }"
          >{{ row.product || `商品 ${row.charge_id}` }}<br /><el-tag
            v-if="productsReady && !supported(row.charge_id)"
            type="warning"
            size="small"
            >当前未支持发货</el-tag
          ></template
        ></el-table-column
      >
      <el-table-column label="金额" width="110"
        ><template #default="{ row }">{{ yuanText(row.amount_cents) }}</template></el-table-column
      >
      <el-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><el-tag :type="row.status === 1 ? 'success' : row.status === 0 ? 'warning' : 'info'">{{
            orderStatuses[row.status] || "未知状态"
          }}</el-tag></template
        ></el-table-column
      >
      <el-table-column label="创建 / 到账时间" min-width="175"
        ><template #default="{ row }"
          >{{ formatTime(row.created_at) }}<br />{{ row.paid_at ? formatTime(row.paid_at) : "—" }}</template
        ></el-table-column
      >
      <el-table-column label="处理人 / 备注" min-width="155"
        ><template #default="{ row }">{{ row.operator || "—" }}<br />{{ row.note || "—" }}</template></el-table-column
      >
      <el-table-column label="付款截图" width="110"
        ><template #default="{ row }"
          ><el-button v-if="row.proof_at" link type="primary" @click="showProof(row)">查看截图</el-button
          ><span v-else class="hint">未上传</span></template
        ></el-table-column
      >
      <el-table-column v-if="manage" label="操作" width="190" fixed="right"
        ><template #default="{ row }"
          ><template v-if="row.status === 0"
            ><el-button
              link
              type="primary"
              :disabled="orderBusy || orderUncertain || !productsReady || !supported(row.charge_id)"
              @click="act(row, 'confirm')"
              >确认到账</el-button
            ><el-button link type="danger" :disabled="orderBusy || orderUncertain" @click="act(row, 'cancel')"
              >取消订单</el-button
            ></template
          ><span v-else class="hint">已处理</span></template
        ></el-table-column
      >
      <template #empty>暂无所选状态的订单</template>
    </el-table>
    <el-dialog v-model="proofOpen" :title="`付款截图 ${proofOrder?.order_no || ''}`" width="min(520px, 92vw)">
      <p v-if="proofOrder" class="hint">
        {{ proofOrder.role || proofOrder.rid }}（{{ proofOrder.rid }}）· {{ proofOrder.product }} ·
        {{ yuanText(proofOrder.amount_cents) }} · 上传于 {{ formatTime(proofOrder.proof_at || 0) }}。截图由玩家自行上传，请以实际收款记录为准。
      </p>
      <el-alert v-if="proofError" :title="proofError" type="error" :closable="false" />
      <img v-if="proofImage" :src="proofImage" alt="付款截图" class="proof-image" />
      <p v-else-if="!proofError" class="hint">加载中…</p>
    </el-dialog>
    <el-alert v-if="result" :title="result.message" :type="result.type" :closable="false" show-icon />
  </div>
</template>
<script setup lang="ts" name="pay-orders">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { api } from "@/auth/runtime";
import { query } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { paymentFeedback, type Order, type OrderReply, type Product } from "@/domain/operations";
import { isSafeImage, orderStatuses, yuanText } from "@/domain/payPages";
import { errorMessage, formatTime } from "@/utils/admin";
import { pendingPay, refreshPendingPay } from "@/composables/usePendingPay";
const auth = useAuthStore();
const manage = computed(() => auth.hasPermission("manage"));
const orderCommand = useCommand("manage", true);
const { busy: orderBusy, uncertain: orderUncertain } = orderCommand;
const status = ref("0");
const limit = ref(100);
const orders = ref<Order[]>([]);
const products = ref<Product[]>([]);
const productsReady = ref(false);
const productError = ref("");
const loading = ref(false);
const error = ref("");
const proofOpen = ref(false);
const proofOrder = ref<Order>();
const proofImage = ref("");
const proofError = ref("");
const result = ref<{ type: "success" | "warning"; message: string }>();
let active = true;
let sequence = 0;
const supported = (id: number) => products.value.some(p => p.id === id);
async function loadProducts() {
  productError.value = "";
  try {
    const data = await api<{ items: Product[] }>("/pay/products");
    if (active) {
      products.value = data.items;
      productsReady.value = true;
    }
  } catch (e) {
    if (active) productError.value = `可发货商品加载失败：${errorMessage(e)}`;
  }
}
async function loadOrders() {
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const data = await api<{ items: Order[] }>(`/pay/orders${query({ status: status.value, limit: limit.value })}`);
    if (active && seq === sequence) orders.value = data.items;
  } catch (e) {
    if (active && seq === sequence) {
      orders.value = [];
      error.value = errorMessage(e);
    }
  } finally {
    if (active && seq === sequence) loading.value = false;
  }
}
async function act(order: Order, action: "confirm" | "cancel") {
  const saved = await orderCommand.execute(
    () => api<OrderReply>(`/pay/orders/${encodeURIComponent(order.order_no)}/${action}`, { method: "POST", body: { note: "" } }),
    action === "confirm"
      ? `确认订单 ${order.order_no}（${order.role || order.rid}，${order.product || order.charge_id}，${yuanText(order.amount_cents)}）已核实收款并发货？`
      : `取消等待付款订单 ${order.order_no}？`
  );
  if (saved && active) {
    result.value =
      action === "confirm" ? paymentFeedback(saved) : { type: "success", message: `订单 ${order.order_no} 已取消，未发货` };
    void loadOrders();
    void refreshPendingPay();
  }
}
async function showProof(order: Order) {
  proofOrder.value = order;
  proofImage.value = "";
  proofError.value = "";
  proofOpen.value = true;
  try {
    const data = await api<{ image: string }>(`/pay/orders/${encodeURIComponent(order.order_no)}/proof`);
    if (!isSafeImage(data.image)) throw Error("截图格式不受支持");
    if (active && proofOrder.value === order) proofImage.value = data.image;
  } catch (e) {
    if (active && proofOrder.value === order) proofError.value = errorMessage(e);
  }
}
async function reviewOrder() {
  await loadOrders();
  if (!error.value) orderCommand.reviewed();
}
// A new order was found by the background poll: show it without a manual refresh.
watch(pendingPay, () => void loadOrders());
onMounted(() => {
  void loadProducts();
  void loadOrders();
});
onBeforeUnmount(() => {
  active = false;
  sequence++;
});
</script>
<style scoped lang="scss">
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 12px 0;
}
.filters .el-select {
  width: 170px;
}
.proof-image {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  margin: 0 auto;
}
</style>
