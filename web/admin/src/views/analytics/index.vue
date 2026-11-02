<template>
  <div class="analytics-page">
    <div class="page-toolbar">
      <h1>运营数据</h1>
      <el-select v-model="days" aria-label="统计天数" style="width: 120px" :disabled="loading" @change="load">
        <el-option v-for="n in analyticsWindows" :key="n" :label="`最近 ${n} 天`" :value="n" />
      </el-select>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <el-alert
      v-if="data && !data.events_from"
      title="还没有埋点数据"
      description="日活、在线时长、在线峰值、留存和协议使用次数要等网关用新版本启动并有玩家进入游戏后才开始记录；新增和付费来自已有的账号、角色和订单表，历史可查。"
      type="warning"
      :closable="false"
      show-icon
    />
    <el-alert
      v-else-if="data && data.events_from > data.from"
      :title="`埋点从 ${data.events_from} 开始记录，此前的日活、在线时长、峰值和留存显示为“—”`"
      type="info"
      :closable="false"
      show-icon
    />
    <div v-loading="loading" class="stat-grid">
      <el-card v-for="card in cards" :key="card.label" shadow="never">
        <div class="stat-label">{{ card.label }}</div>
        <strong>{{ card.value }}</strong>
        <div v-if="card.sub" class="hint">{{ card.sub }}</div>
      </el-card>
    </div>
    <el-card v-if="data" shadow="never">
      <template #header>趋势</template>
      <div class="chart"><ECharts :option="trend" /></div>
    </el-card>
    <el-card v-if="data" shadow="never">
      <template #header>每日数据（北京时间）</template>
      <el-table :data="rows" stripe size="small" max-height="460" empty-text="暂无数据">
        <el-table-column prop="day" label="日期" width="110" fixed />
        <el-table-column label="日活" width="80"
          ><template #default="{ row }">{{ figure(row.active, row.day) }}</template></el-table-column
        >
        <el-table-column label="登录账号" width="90"
          ><template #default="{ row }">{{ figure(row.login_accounts, row.day) }}</template></el-table-column
        >
        <el-table-column prop="new_accounts" label="新增账号" width="90" />
        <el-table-column prop="new_roles" label="新增角色" width="90" />
        <el-table-column label="平均在线" width="110"
          ><template #default="{ row }">{{ figure(row.avg_session_sec, row.day, duration) }}</template></el-table-column
        >
        <el-table-column label="在线峰值" width="90"
          ><template #default="{ row }">{{ figure(row.peak_online, row.day) }}</template></el-table-column
        >
        <el-table-column prop="orders_created" label="开单" width="70" />
        <el-table-column prop="orders_paid" label="到账" width="70" />
        <el-table-column label="转化" width="70"
          ><template #default="{ row }">{{ payConversion(row) }}</template></el-table-column
        >
        <el-table-column prop="paying_roles" label="付费角色" width="90" />
        <el-table-column label="收入（元）" min-width="110"
          ><template #default="{ row }">{{ yuan(row.paid_cents) }}</template></el-table-column
        >
      </el-table>
      <p class="hint">日活是当天进入过游戏的角色数；平均在线是当天结束的连接的平均时长；转化是当天到账订单占当天开单的比例。</p>
    </el-card>
    <el-card v-if="data" shadow="never">
      <template #header>新角色留存</template>
      <el-table :data="retention" stripe size="small" max-height="360" empty-text="暂无数据">
        <el-table-column prop="day" label="创角日期" width="110" />
        <el-table-column prop="cohort" label="新角色" width="90" />
        <el-table-column label="次日" width="120"
          ><template #default="{ row }">{{ keep(row.d1, row.cohort) }}</template></el-table-column
        >
        <el-table-column label="3 日" width="120"
          ><template #default="{ row }">{{ keep(row.d3, row.cohort) }}</template></el-table-column
        >
        <el-table-column label="7 日" min-width="120"
          ><template #default="{ row }">{{ keep(row.d7, row.cohort) }}</template></el-table-column
        >
      </el-table>
      <p class="hint">次日留存 = 创角后第 2 天又进入游戏的角色数 ÷ 当天新角色数；那一天还没结束，或在埋点开始之前，显示“—”。</p>
    </el-card>
    <el-card v-if="data" shadow="never">
      <template #header>最常用的协议（前 50）</template>
      <el-table :data="data.cmds" stripe size="small" max-height="420" empty-text="暂无记录">
        <el-table-column prop="cmd" label="协议号" width="110" />
        <el-table-column prop="hits" label="次数" width="120" />
        <el-table-column prop="roles" label="角色数" min-width="120" />
      </el-table>
      <p class="hint">不含心跳等定时请求；协议号对应的玩法见 docs/CLIENT_COVERAGE.md。</p>
    </el-card>
  </div>
</template>
<script setup lang="ts" name="analytics">
import { computed, onMounted, ref } from "vue";
import { api } from "@/auth/runtime";
import ECharts from "@/components/ECharts/index.vue";
import {
  analyticsWindows,
  duration,
  eventFigure,
  payConversion,
  rate,
  totals,
  trendOption,
  yuan,
  type Analytics
} from "@/domain/analytics";
import { errorMessage } from "@/utils/admin";
const days = ref<number>(14);
const data = ref<Analytics>();
const loading = ref(false);
const error = ref("");
let seq = 0;
const eventsFrom = computed(() => data.value?.events_from ?? "");
const rows = computed(() => [...(data.value?.days ?? [])].reverse());
const retention = computed(() => [...(data.value?.retention ?? [])].reverse());
const trend = computed(() => (data.value ? trendOption(data.value) : {}));
const today = computed(() => data.value?.days[data.value.days.length - 1]);
const sum = computed(() => totals(data.value?.days ?? []));
const figure = (value: number, day: string, format?: (n: number) => string) => eventFigure(value, day, eventsFrom.value, format);
const keep = (kept: number | null, cohort: number) => (kept == null ? "—" : `${kept}（${rate(kept, cohort)}）`);
const cards = computed(() => {
  const t = today.value;
  const n = (value: number | undefined) => (value == null ? "—" : value.toLocaleString("zh-CN"));
  return [
    { label: "今日日活", value: t ? figure(t.active, t.day) : "—" },
    { label: "今日新增角色", value: n(t?.new_roles), sub: `近 ${days.value} 天 ${n(sum.value.newRoles)}` },
    { label: "今日在线峰值", value: t ? figure(t.peak_online, t.day) : "—" },
    { label: "今日收入（元）", value: t ? yuan(t.paid_cents) : "—", sub: `近 ${days.value} 天 ${yuan(sum.value.paidCents)}` }
  ];
});
async function load() {
  const mine = ++seq;
  loading.value = true;
  error.value = "";
  try {
    const result = await api<Analytics>(`/analytics?days=${days.value}`);
    if (mine === seq) data.value = result;
  } catch (e) {
    if (mine === seq) error.value = `运营数据加载失败：${errorMessage(e)}`;
  } finally {
    if (mine === seq) loading.value = false;
  }
}
onMounted(load);
</script>
<style scoped lang="scss">
.analytics-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
}
.stat-label {
  margin-bottom: 12px;
  color: var(--el-text-color-secondary);
}
strong {
  font-size: 32px;
}
.chart {
  height: 320px;
}
</style>
