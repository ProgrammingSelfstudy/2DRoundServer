<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>充值管理</h1>
      <el-button @click="refreshAll" :loading="loading">刷新充值数据</el-button>
    </div>
    <el-alert
      title="人工收款确认"
      description="请先核对实际收款，再确认到账发货。未收到款的订单不要确认；本页面不会接入或发起第三方支付。"
      type="info"
      :closable="false"
    />
    <el-alert v-if="configError" :title="configError" type="error" :closable="false" />
    <h2>收款码与支付说明</h2>
    <el-form label-position="top" :disabled="configBusy || !configReady" @submit.prevent="saveNotice">
      <div class="qr-grid">
        <el-card v-for="channel in channels" :key="channel.key" shadow="never">
          <template #header>{{ channel.label }}收款码</template>
          <img
            v-if="safeImage(config[channel.key])"
            :src="config[channel.key]"
            :alt="`${channel.label}收款码`"
            class="qr-image"
          />
          <p v-else class="hint">尚未设置收款码</p>
          <template v-if="manage">
            <label :for="channel.key">选择{{ channel.label }}图片（最多 600 KB）</label>
            <input
              :id="channel.key"
              type="file"
              accept="image/png,image/jpeg,image/webp"
              :disabled="configBusy || !configReady"
              @change="selectFile($event, channel.key)"
            />
            <div class="actions">
              <el-button type="primary" :loading="configBusy" @click="saveQr(channel.key)"
                >保存{{ channel.label }}收款码</el-button
              ><el-button v-if="config[channel.key]" type="danger" :disabled="configBusy" @click="clearQr(channel.key)"
                >清除{{ channel.label }}收款码</el-button
              >
            </div>
          </template>
        </el-card>
      </div>
      <el-form-item label="付款页提示（最多 300 字）" label-for="pay-notice"
        ><el-input id="pay-notice" v-model="notice" :readonly="!manage" type="textarea" :rows="3" maxlength="300" show-word-limit
      /></el-form-item>
      <el-button v-if="manage" type="primary" native-type="submit" :loading="configBusy">保存支付说明</el-button>
    </el-form>
    <el-alert v-if="configCommand.error.value" :title="configCommand.error.value" type="error" :closable="false" />
    <p v-if="!manage" class="hint">当前账号仅可查看收款配置和订单；配置、补单、确认和取消仅限超级管理员。</p>
    <h2>订单</h2>
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
      <el-button :loading="loading" @click="loadOrders">刷新订单</el-button>
    </div>
    <p class="hint">
      当前显示所选状态最近 {{ limit }} 笔订单，可调整条数或状态。仅支持列表中的可发货商品，购买资格会在提交时校验。
    </p>
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
        ><template #default="{ row }">{{ money(row.amount_cents) }}</template></el-table-column
      >
      <el-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><el-tag :type="row.status === 1 ? 'success' : row.status === 0 ? 'warning' : 'info'">{{
            statuses[row.status] || "未知状态"
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
        {{ proofOrder.role || proofOrder.rid }}（{{ proofOrder.rid }}）· {{ proofOrder.product }} · {{ money(proofOrder.amount_cents) }} ·
        上传于 {{ formatTime(proofOrder.proof_at || 0) }}。截图由玩家自行上传，请以实际收款记录为准。
      </p>
      <el-alert v-if="proofError" :title="proofError" type="error" :closable="false" />
      <img v-if="proofImage" :src="proofImage" alt="付款截图" class="proof-image" />
      <p v-else-if="!proofError" class="hint">加载中…</p>
    </el-dialog>
    <el-card v-if="manage" shadow="never" class="manual">
      <template #header>手工补单（玩家已线下付款）</template>
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
        <p class="hint">未勾选时只创建等待付款订单，需在列表单独确认到账；此时服务端不保存补单备注。发货后不可通过取消撤回。</p>
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
        description="订单或发货可能已经写入。请先刷新订单核对，再决定是否新建另一笔。"
        type="warning"
        :closable="false"
        ><el-button @click="reviewManual">已核对订单，重新填写</el-button></el-alert
      >
    </el-card>
    <el-alert v-if="result" :title="result.message" :type="result.type" :closable="false" show-icon />
  </div>
</template>
<script setup lang="ts" name="pay">
import IntegerInput from "@/components/Admin/IntegerInput.vue";
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { query, type RoleRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { manualOrder, paymentFeedback, validateQr, type Order, type OrderReply, type Product } from "@/domain/operations";
import { errorMessage, formatTime } from "@/utils/admin";
import { pendingPay, refreshPendingPay } from "@/composables/usePendingPay";
import RoleLookup from "@/components/Admin/RoleLookup.vue";
const auth = useAuthStore();
const manage = computed(() => auth.hasPermission("manage"));
const configCommand = useCommand("manage");
const orderCommand = useCommand("manage", true);
const manualCommand = useCommand("manage", true);
const { busy: configBusy } = configCommand;
const { busy: orderBusy, uncertain: orderUncertain } = orderCommand;
const { busy: manualBusy, uncertain: manualUncertain } = manualCommand;
type Channel = "qr_wechat" | "qr_alipay";
const channels: { key: Channel; label: string }[] = [
  { key: "qr_wechat", label: "微信" },
  { key: "qr_alipay", label: "支付宝" }
];
const config = reactive({ qr_wechat: "", qr_alipay: "", notice: "" });
const files = reactive<Partial<Record<Channel, File>>>({});
const configReady = ref(false);
const configError = ref("");
const notice = ref("");
const status = ref("0");
const limit = ref(100);
const orders = ref<Order[]>([]);
const products = ref<Product[]>([]);
const productsReady = ref(false);
const productError = ref("");
const loading = ref(false);
const error = ref("");
const rid = ref<number>();
const charge = ref<number>();
const note = ref("");
const confirmNow = ref(true);
const roleLabel = ref("");
const formError = ref("");
const proofOpen = ref(false);
const proofOrder = ref<Order>();
const proofImage = ref("");
const proofError = ref("");
const result = ref<{ type: "success" | "warning"; message: string }>();
const statuses: Record<number, string> = { 0: "等待付款", 1: "已到账", 2: "已取消" };
let active = true;
let sequence = 0;
const money = (cents: number) => (cents / 100).toLocaleString("zh-CN", { style: "currency", currency: "CNY" });
const supported = (id: number) => products.value.some(p => p.id === id);
const safeImage = (value: string) => /^data:image\/(png|jpeg|webp);base64,/.test(value);
async function loadConfig() {
  configError.value = "";
  try {
    const data = await api<typeof config>("/pay/config");
    if (active) {
      Object.assign(config, data);
      notice.value = data.notice;
      configReady.value = true;
    }
  } catch (e) {
    if (active) configError.value = errorMessage(e);
  }
}
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
async function refreshAll() {
  await Promise.all([loadConfig(), loadProducts(), loadOrders()]);
}
function selectFile(event: Event, key: Channel) {
  files[key] = (event.target as HTMLInputElement).files?.[0];
}
function readImage(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(Error("读取图片失败"));
    reader.readAsDataURL(file);
  });
}
async function saveQr(key: Channel) {
  const file = files[key];
  try {
    if (!file) throw Error("请先选择收款码图片");
    validateQr(file);
  } catch (e) {
    configCommand.error.value = errorMessage(e);
    return;
  }
  const saved = await configCommand.execute(async () =>
    api("/pay/config", { method: "PUT", body: { key, value: await readImage(file!) } })
  );
  if (saved && active) {
    ElMessage.success("收款码已保存");
    void loadConfig();
  }
}
async function clearQr(key: Channel) {
  const saved = await configCommand.execute(
    () => api("/pay/config", { method: "PUT", body: { key, value: "" } }),
    `清除${key === "qr_wechat" ? "微信" : "支付宝"}收款码？`
  );
  if (saved && active) {
    ElMessage.success("收款码已清除");
    void loadConfig();
  }
}
async function saveNotice() {
  if ([...notice.value].length > 300) {
    configCommand.error.value = "支付说明最多 300 字";
    return;
  }
  const value = notice.value;
  const saved = await configCommand.execute(() => api("/pay/config", { method: "PUT", body: { key: "notice", value } }));
  if (saved && active) {
    config.notice = value;
    ElMessage.success("支付说明已保存");
  }
}
async function act(order: Order, action: "confirm" | "cancel") {
  const saved = await orderCommand.execute(
    () => api<OrderReply>(`/pay/orders/${encodeURIComponent(order.order_no)}/${action}`, { method: "POST", body: { note: "" } }),
    action === "confirm"
      ? `确认订单 ${order.order_no}（${order.role || order.rid}，${order.product || order.charge_id}，${money(order.amount_cents)}）已核实收款并发货？`
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
    // Only image data URLs are rendered, the same rule the QR codes follow.
    if (!safeImage(data.image)) throw Error("截图格式不受支持");
    if (active && proofOrder.value === order) proofImage.value = data.image;
  } catch (e) {
    if (active && proofOrder.value === order) proofError.value = errorMessage(e);
  }
}
function selectRole(role: RoleRow) {
  rid.value = role.rid;
  roleLabel.value = `${role.name}（${role.rid} / ${role.srv_id}，账号 ${role.account}）`;
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
    rid.value = undefined;
    roleLabel.value = "";
    note.value = "";
    void loadOrders();
    void refreshPendingPay();
  }
}
async function reviewOrder() {
  await loadOrders();
  if (!error.value) orderCommand.reviewed();
}
async function reviewManual() {
  await loadOrders();
  if (!error.value) {
    manualCommand.reviewed();
    rid.value = undefined;
    note.value = "";
    roleLabel.value = "";
  }
}
// A new order was found by the background poll: show it without a manual refresh.
watch(pendingPay, () => void loadOrders());
onMounted(refreshAll);
onBeforeUnmount(() => {
  active = false;
  sequence++;
});
</script>
<style scoped lang="scss">
.qr-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  margin: 16px 0;
}
.qr-image {
  width: 160px;
  height: 160px;
  object-fit: contain;
  border: 1px solid var(--el-border-color);
}
label {
  display: block;
  margin: 12px 0;
}
.actions,
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
.manual {
  margin: 24px 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}
@media (max-width: 650px) {
  .qr-grid,
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
