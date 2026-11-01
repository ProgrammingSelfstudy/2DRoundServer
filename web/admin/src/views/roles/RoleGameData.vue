<template>
  <el-alert v-if="tableCatalog.error.value" :title="tableCatalog.error.value" type="error" :closable="false" /><el-button v-if="tableCatalog.error.value" @click="tableCatalog.load()">重试表目录</el-button>
  <el-tabs v-model="tab" :before-leave="() => !busy && !uncertain">
    <el-tab-pane label="充值状态与订单" name="charge" />
    <el-tab-pane label="模块数据" name="modules" />
    <el-tab-pane
      v-for="g in tableGroups"
      :key="g"
      :label="g === '邮件' ? '收件' : g === '英雄' ? '英雄与外观' : g"
      :name="`table-${g}`"
    />
  </el-tabs>
  <TableBrowser v-if="tab.startsWith('table-')" scope="role" :owner="rid" :group="tab.slice(6)" />
  <div v-else v-loading="loading">
    <el-alert v-if="error || commandError" :title="error || commandError" type="error" :closable="false" />
    <el-alert
      v-if="uncertain"
      title="操作结果待核对：请刷新状态并核对审计，确认后解除提交锁；不要直接重试。"
      type="warning"
      :closable="false"
    />
    <el-button v-if="uncertain" :disabled="busy || loading" @click="reviewed">已核对结果，解除锁定</el-button>
    <el-button :disabled="busy || loading" @click="load">刷新当前页</el-button>
    <template v-if="tab === 'charge' && charge">
      <h3>实际充值汇总</h3>
      <el-descriptions :column="2" border
        ><el-descriptions-item label="累计实付（分）">{{ charge.charge.total_cents }}</el-descriptions-item
        ><el-descriptions-item label="充值VIP经验">{{ charge.charge.vip_exp }}</el-descriptions-item
        ><el-descriptions-item label="首次付款">{{ formatTime(charge.charge.first_at) }}</el-descriptions-item
        ><el-descriptions-item label="最近付款">{{ formatTime(charge.charge.last_at) }}</el-descriptions-item></el-descriptions
      >
      <el-select v-model="selected" filterable aria-label="充值模块" @change="selectCharge" style="width: min(100%, 480px)"
        ><el-option v-for="m in charge.modules" :key="m.key" :value="m.key" :label="`${m.module?.title}（${m.key}）`"
      /></el-select>
    </template>
    <template v-if="tab === 'modules'">
      <p>列出已注册模块及保存状态；未保存不创建默认行。动态首充窗口按实际窗口列出。</p>
      <el-select v-model="selected" filterable aria-label="玩家模块" @change="loadModule" style="width: min(100%, 480px)"
        ><el-option
          v-for="m in choices"
          :key="m.key"
          :value="m.key"
          :label="`${m.title}（${m.key}） · ${saved.has(m.key) ? '已保存' : '未保存'}`"
      /></el-select>
      <el-alert
        v-if="unknown.length"
        :title="`存在未注册保存行，仅列出键：${unknown.join('、')}`"
        type="warning"
        :closable="false"
      />
    </template>
    <template v-if="state">
      <p>{{ state.module?.title }} · {{ state.exists ? "已保存" : "未保存" }} · {{ state.module?.window }}</p>
      <template v-if="state.view"
        ><h3>计算视图（只读）</h3>
        <ChargeView :state="state"
      /></template>
      <el-collapse
        ><el-collapse-item title="原始已保存 JSON（只读）"><JsonView :value="state.data" /></el-collapse-item
      ></el-collapse>
      <el-form v-if="canReset(auth.admin?.role, state.module)" :disabled="busy || uncertain || loading" @submit.prevent="reset">
        <p>仅重置白名单项目，需角色离线；重置不发奖。月卡重置以当前实付金额为新基数。</p>
        <el-select v-model="item" aria-label="重置项目" style="width: 300px"
          ><el-option v-for="r in state.module?.resets" :key="r.item" :value="r.item" :label="`${r.title}（${r.item}）`"
        /></el-select>
        <el-input v-model="why" placeholder="重置原因（必填，最多200字）" aria-label="重置原因" maxlength="200" />
        <el-button type="danger" native-type="submit" :disabled="!state.exists" :loading="busy">重置所选项</el-button>
      </el-form>
      <el-button v-if="onlineConflict && auth.hasPermission('write')" :disabled="busy" @click="kick">踢下线</el-button>
    </template>
    <template v-if="tab === 'charge' && charge">
      <h3>该角色全部订单</h3>
      <RecordTable :rows="charge.orders.items" :columns="orderColumns" :loading="loading" max-height="400" />
      <el-pagination
        :current-page="page"
        :page-size="50"
        :total="charge.orders.total"
        layout="total, prev, pager, next"
        @current-change="changePage"
      />
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch, onBeforeUnmount } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { ApiError } from "@/api/client";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { canReset, resetPayload, gameError, type ModuleInfo, type ModuleState, type TableInfo } from "@/domain/gameAdmin";
import ChargeView from "@/components/Admin/ChargeView.vue";
import { formatTime } from "@/utils/admin";
import JsonView from "@/components/Admin/JsonView.vue";
import TableBrowser from "@/components/Admin/TableBrowser.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const tableCatalog = useCollection<TableInfo>("/tables",200,false);
const tableGroups = computed(() => [...new Set(tableCatalog.rows.value.filter(t => t.scope === "role").map(t => t.group))]);
onMounted(() => void tableCatalog.load());
const props = defineProps<{ rid: number }>();
const emit = defineEmits<{locked:[value:boolean]}>();
const auth = useAuthStore();
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("manage", true, e => e.status === 502);
const kickCommand = useCommand("write");
watch([busy,uncertain,kickCommand.busy], () => emit("locked", busy.value || uncertain.value || kickCommand.busy.value));
interface Charge {
  charge: { total_cents: number; vip_exp: number; first_at: number; last_at: number };
  modules: ModuleState[];
  orders: { items: Record<string, unknown>[]; total: number };
}
const tab = ref("charge"),
  loading = ref(false),
  error = ref(""),
  selected = ref(""),
  item = ref(""),
  why = ref(""),
  page = ref(1),
  onlineConflict = ref(false);
const choices = ref<ModuleInfo[]>([]),
  saved = ref(new Set<string>()),
  unknown = ref<string[]>([]),
  state = ref<ModuleState>(),
  charge = ref<Charge>();
const orderColumns: Column[] = [
  { key: "order_no", label: "订单号" },
  { key: "charge_id", label: "商品ID" },
  { key: "amount_cents", label: "订单金额（分）" },
  { key: "status", label: "状态", format: v => ({ 0: "待付款", 1: "已付款", 2: "已取消" })[v as number] || String(v) },
  { key: "created_at", label: "创建时间", time: true },
  { key: "paid_at", label: "付款时间", time: true },
  { key: "operator", label: "处理人" },
  { key: "note", label: "备注" }
];
let seq = 0,
  active = true;
function selectCharge() {
  state.value = charge.value?.modules.find(m => m.key === selected.value);
  item.value = "";
  why.value = "";
  onlineConflict.value = false;
}
async function loadModule() {
  const token = ++seq;
  state.value = undefined;
  item.value = "";
  why.value = "";
  onlineConflict.value = false;
  error.value = "";
  loading.value = true;
  try {
    const r = await api<ModuleState>(`/roles/${props.rid}/modules/${encodeURIComponent(selected.value)}`);
    if (active && token === seq) state.value = r;
  } catch (e) {
    if (active && token === seq) error.value = e instanceof Error ? e.message : "读取失败";
  } finally {
    if (active && token === seq) loading.value = false;
  }
}
async function load() {
  const token = ++seq;
  error.value = "";
  state.value = undefined;
  charge.value = undefined;
  loading.value = true;
  try {
    if (tab.value === "charge") {
      const r = await api<Charge>(`/roles/${props.rid}/charge?limit=50&offset=${(page.value - 1) * 50}`);
      if (!active || token !== seq) return;
      charge.value = r;
      if (!r.modules.some(m => m.key === selected.value)) selected.value = r.modules[0]?.key || "";
      selectCharge();
    } else if (tab.value === "modules") {
      const [registry, rows, chargeKeys] = await Promise.all([
        api<{ items: ModuleInfo[] }>("/modules"),
        api<{ items: { key: string; registered: boolean }[] }>(`/roles/${props.rid}/modules`),
        api<Charge>(`/roles/${props.rid}/charge?limit=1`)
      ]);
      if (!active || token !== seq) return;
      saved.value = new Set(rows.items.map(m => m.key));
      unknown.value = rows.items.filter(m => !m.registered).map(m => m.key);
      choices.value = registry.items
        .filter(m => m.scope === "role")
        .flatMap(m =>
          m.key.endsWith("*")
            ? [...new Set([...rows.items.map(r => r.key), ...chargeKeys.modules.map(r => r.key)])]
                .filter(key => key.startsWith(m.key.slice(0, -1)))
                .map(key => ({ ...m, key }))
            : [m]
        );
      if (!choices.value.some(m => m.key === selected.value)) selected.value = choices.value[0]?.key || "";
      await loadModule();
    }
  } catch (e) {
    if (active && token === seq) error.value = e instanceof Error ? e.message : "读取失败";
  } finally {
    if (active && token === seq) loading.value = false;
  }
}
async function reset() {
  if (!state.value?.module) return;
  let body;
  try {
    body = resetPayload(state.value.module, item.value, why.value);
  } catch (e) {
    error.value = (e as Error).message;
    return;
  }
  const key = state.value.key,
    rid = props.rid;
  onlineConflict.value = false;
  const r = await execute(async () => {
    try {
      return await api(`/roles/${rid}/modules/${encodeURIComponent(key)}/reset`, { method: "POST", body });
    } catch (e) {
      if (e instanceof ApiError) {
        onlineConflict.value = e.status === 409 && (e.message.includes("online") || e.message.includes("玩家在线"));
        error.value = gameError(e.status, e.message, true);
      }
      throw e;
    }
  }, `重置角色 ${rid} 的 ${key} / ${body.item}？原因：${body.reason}。该操作会改变领取或购买状态。`);
  if (r && active) {
    ElMessage.success("白名单重置已完成");
    await load();
  }
}
async function kick() {
  const r = await kickCommand.execute(
    () => api(`/roles/${props.rid}/kick`, { method: "POST", body: {} }),
    `请求踢下线角色 ${props.rid}？`
  );
  if (r) {
    ElMessage.success("踢线请求已发出，请刷新确认离线后再重置");
    onlineConflict.value = false;
  }
}
function changePage(n: number) {
  page.value = n;
  void load();
}
watch(
  () => [props.rid, tab.value],
  () => {
    page.value = 1;
    selected.value = "";
    state.value = undefined;
    charge.value = undefined;
    void load();
  },
  { immediate: true }
);
onBeforeUnmount(() => {
  emit("locked",false);
  active = false;
  seq++;
});
</script>
