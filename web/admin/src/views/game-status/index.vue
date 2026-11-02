<template>
  <div class="management-page">
    <h1>{{ title }}</h1>
    <el-select v-model="srv" aria-label="玩法区服" filterable :disabled="busy || uncertain" style="width: 300px"
      ><el-option v-for="s in servers" :key="s.srv_id" :value="s.srv_id" :label="`${s.name}（${s.srv_id}）`"
    /></el-select>
    <el-button :disabled="busy || loading || !srv" @click="load">刷新状态</el-button>
    <el-alert v-if="error || commandError" :title="error || commandError" type="error" :closable="false" />
    <el-alert v-if="uncertain" title="写入结果待核对，请刷新状态并核对审计后解除锁定。" type="warning" :closable="false" />
    <el-button v-if="uncertain" :disabled="busy || loading" @click="reviewed">已核对结果，解除锁定</el-button>
    <div v-if="srv" v-loading="loading">
      <el-tabs v-if="tabs.length > 1" v-model="tab"
        ><el-tab-pane v-for="t in tabs" :key="t.name" :label="t.label" :name="t.name"
      /></el-tabs>
      <template v-if="tab === 'overview' && overview">
        <el-descriptions :column="2" border
          ><el-descriptions-item label="世界等级">{{ overview.world_lev }}</el-descriptions-item
          ><el-descriptions-item label="开服第几天">{{ overview.open_day }}</el-descriptions-item
          ><el-descriptions-item label="开服时间">{{ formatTime(overview.open_time) }}</el-descriptions-item
          ><el-descriptions-item label="竞技场本季">{{ overview.arena?.season }}</el-descriptions-item
          ><el-descriptions-item label="本季开始">{{ formatTime(overview.arena?.start_time) }}</el-descriptions-item
          ><el-descriptions-item label="本季结束">{{
            formatTime(overview.arena?.end_time)
          }}</el-descriptions-item></el-descriptions
        >
        <h3>世界等级</h3>
        <el-descriptions :column="3" border
          ><el-descriptions-item label="配置覆盖值">{{ overview.world_level?.override }}</el-descriptions-item
          ><el-descriptions-item label="游戏计算值">{{ overview.world_level?.computed }}</el-descriptions-item
          ><el-descriptions-item label="当前有效值">{{ overview.world_level?.effective }}</el-descriptions-item
          ><el-descriptions-item label="覆盖生效">{{ overview.world_level?.active ? "是" : "否" }}</el-descriptions-item
          ><el-descriptions-item label="覆盖到期">{{
            overview.world_level?.until ? formatTime(overview.world_level.until) : "长期"
          }}</el-descriptions-item></el-descriptions
        >
        <el-form v-if="auth.hasPermission('manage')" :disabled="busy || uncertain || loading" @submit.prevent="saveWorld">
          <el-form-item label="覆盖等级（0关闭）"
            ><el-input-number v-model="override" :min="0" :max="200" :precision="0"
          /></el-form-item>
          <el-form-item label="到期时间（空为长期）"
            ><el-input v-model="until" type="datetime-local" aria-label="世界等级到期时间"
          /></el-form-item>
          <el-form-item label="修改原因"><el-input v-model="why" maxlength="200" aria-label="世界等级修改原因" /></el-form-item>
          <el-button native-type="submit" type="primary" :loading="busy">保存世界等级覆盖</el-button>
        </el-form>
      </template>
      <template v-if="tab === 'rank'">
        <el-select v-model="rankKey" aria-label="排名模块" filterable @change="loadRank" style="width: 360px"
          ><el-option v-for="m in ranked" :key="m.key" :value="m.key" :label="`${m.title}（${m.key}）`"
        /></el-select>
        <p>每个排名分类最多200条，沿用游戏排名顺序。</p>
        <el-table v-if="rank" :data="rank.items" border max-height="500"
          ><el-table-column prop="rank" label="名次" width="90" /><el-table-column label="排名数据（含分类）" min-width="350"
            ><template #default="{ row }"><JsonView :value="row.data" /></template></el-table-column
        ></el-table>
      </template>
      <template v-if="tab === 'seasons'">
        <el-table :data="seasons" border
          ><el-table-column prop="title" label="玩法" min-width="160" /><el-table-column
            prop="season.key"
            label="赛季/届次"
          /><el-table-column label="开始"
            ><template #default="{ row }">{{ formatTime(row.season.start_time) }}</template></el-table-column
          ><el-table-column label="结束"
            ><template #default="{ row }">{{ formatTime(row.season.end_time) }}</template></el-table-column
          ><el-table-column prop="season.phase" label="阶段" /><el-table-column label="状态"
            ><template #default="{ row }">{{
              row.season.settled ? "已结算" : row.season.due ? "有到期待补结算项" : "未到期/进行中"
            }}</template></el-table-column
          ><el-table-column label="操作" width="180"
            ><template #default="{ row }"
              ><el-button @click="seasonDetail = row">详情</el-button
              ><el-button
                v-if="canSettle(auth.admin?.role, row.key)"
                :disabled="busy || uncertain || loading"
                @click="
                  settleTarget = row.key;
                  settleReason = '';
                "
                >到期补结算</el-button
              ></template
            ></el-table-column
          ></el-table
        >
        <p>只允许补自然到期且未结算的项目；不能提前结束或推进赛季。</p>
        <el-dialog :model-value="!!seasonDetail" title="赛季状态" @close="seasonDetail = undefined"
          ><JsonView :value="seasonDetail"
        /></el-dialog>
      </template>
      <template v-if="tab === 'global'">
        <el-select v-model="globalKey" aria-label="全服模块" filterable style="width: 420px"
          ><el-option
            v-for="m in visibleSnapshots"
            :key="m.key"
            :value="m.key"
            :label="`${moduleTitle(m.key)}（${m.key}） · ${m.exists ? '已保存' : '未保存'}`"
        /></el-select>
        <p>只读快照；各服务分别读取，不代表全服同一时刻。动态公会模块只列出已保存实例。</p>
        <JsonView v-if="globalState" :value="globalState" />
      </template>
      <template v-if="tab === 'tables'">
        <el-radio-group v-model="tableScope"
          ><el-radio-button value="server">区服表</el-radio-button
          ><el-radio-button value="guild">公会表</el-radio-button></el-radio-group
        >
        <template v-if="tableScope === 'guild'"
          ><el-input v-model="guildInput" placeholder="公会GID" aria-label="公会GID" style="width: 220px" /><el-button
            @click="selectGuild"
            >查看公会表</el-button
          ></template
        >
        <TableBrowser
          v-if="tableScope === 'server' || guildID"
          :key="`${tableScope}-${srv}-${guildID}`"
          :scope="tableScope"
          :owner="tableScope === 'server' ? srv : guildID"
        />
      </template>
    </div>
    <el-dialog
      :model-value="!!settleTarget"
      title="确认到期补结算"
      :close-on-click-modal="false"
      :before-close="closeSettle"
      @closed="settleTarget = ''"
    >
      <p>区服 {{ srv }} / {{ moduleTitle(settleTarget) }}。仅补自然到期项目，不提前结算。</p>
      <el-input v-model="settleReason" maxlength="200" aria-label="结算原因" placeholder="必填原因，最多200字" :disabled="busy" />
      <template #footer
        ><el-button :disabled="busy" @click="settleTarget = ''">取消</el-button
        ><el-button type="danger" :disabled="uncertain || busy" @click="settle">确认补结算</el-button></template
      >
    </el-dialog>
  </div>
</template>
<script setup lang="ts" name="game-status">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { useRoute } from "vue-router";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { ApiError } from "@/api/client";
import { getServers, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { selectedServer } from "@/composables/useSelectedServer";
import { categoryOf, categoryTitle } from "@/domain/moduleCategories";
import {
  canSettle,
  reason,
  worldPayload,
  settleFeedback,
  gameError,
  statusSelection,
  type ModuleInfo,
  type ModuleState,
  type Season
} from "@/domain/gameAdmin";
import { formatTime, errorMessage } from "@/utils/admin";
import JsonView from "@/components/Admin/JsonView.vue";
import TableBrowser from "@/components/Admin/TableBrowser.vue";
const route = useRoute(),
  auth = useAuthStore(),
  { busy, uncertain, error: commandError, execute, reviewed } = useCommand("manage", true);
const catalog = useCollection<ModuleInfo>("/modules", 200, false);
// This page is several: the route says which part it shows (meta.section) and, for the server-wide modules, of which kind
// (meta.category), so each address is one short list instead of every module at once.
const section = computed(() => String(route.meta.section || "overview"));
const category = computed(() => String(route.meta.category || ""));
const title = computed(() => `玩法状态 · ${route.meta.title || "区服概况"}`);
const tabs = computed(() =>
  section.value === "tables"
    ? [{ name: "tables", label: "区服 / 公会只读表" }]
    : section.value === "category"
      ? [
          { name: "rank", label: "排名" },
          { name: "seasons", label: "赛季" },
          { name: "global", label: "全服数据" }
        ]
      : [{ name: "overview", label: "区服概况" }]
);
const inCategory = (key: string) => section.value !== "category" || categoryOf(key) === category.value;
const servers = ref<ServerRow[]>([]),
  srv = ref(selectedServer.value),
  tab = ref(tabs.value[0].name),
  loading = ref(false),
  error = ref("");
const overview = ref<any>(),
  snapshots = ref<ModuleState[]>([]),
  rank = ref<{ items: { rank: number; data: unknown }[] }>(),
  rankKey = ref(""),
  globalKey = ref("");
const override = ref<number>(0),
  until = ref(""),
  why = ref(""),
  settleTarget = ref(""),
  settleReason = ref(""),
  seasonDetail = ref<unknown>();
const tableScope = ref<"server" | "guild">("server"),
  guildInput = ref(""),
  guildID = ref("");
const ranked = computed(() => catalog.rows.value.filter(m => m.scope === "global" && m.has_rank && inCategory(m.key)));
const visibleSnapshots = computed(() => snapshots.value.filter(m => inCategory(m.key)));
const seasons = computed(() =>
  visibleSnapshots.value
    .filter(m => m.view?.season?.supported)
    .map(m => ({ key: m.key, title: moduleTitle(m.key), season: m.view!.season! }))
);
const globalState = computed(() => visibleSnapshots.value.find(m => m.key === globalKey.value));
function moduleTitle(key: string) {
  return catalog.rows.value.find(m => m.key === key || (m.key.endsWith("*") && key.startsWith(m.key.slice(0, -1))))?.title || key;
}
let seq = 0,
  active = true;
async function load() {
  const token = ++seq;
  error.value = "";
  loading.value = true;
  overview.value = undefined;
  snapshots.value = [];
  rank.value = undefined;
  try {
    if (!srv.value) return;
    const base = `/servers/${encodeURIComponent(srv.value)}`;
    if (tab.value === "overview") {
      const r = await api<any>(base + "/overview");
      if (active && token === seq) {
        overview.value = r;
        override.value = r.world_level?.override || 0;
        until.value = r.world_level?.until ? toLocal(r.world_level.until) : "";
        why.value = "";
      }
    } else if (tab.value === "global" || tab.value === "seasons") {
      const r = await api<{ items: ModuleState[] }>(base + "/modules");
      if (active && token === seq) {
        snapshots.value = r.items;
        const own = r.items.filter(m => inCategory(m.key));
        if (!own.some(m => m.key === globalKey.value)) globalKey.value = own[0]?.key || "";
      }
    } else if (tab.value === "rank") {
      if (!ranked.value.some(m => m.key === rankKey.value)) rankKey.value = ranked.value[0]?.key || "";
      await loadRank();
    }
  } catch (e) {
    if (active && token === seq) error.value = e instanceof ApiError ? gameError(e.status, e.message) : errorMessage(e);
  } finally {
    if (active && token === seq) loading.value = false;
  }
}
function toLocal(n: number) {
  const d = new Date(n * 1000);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
async function loadRank() {
  const token = ++seq;
  rank.value = undefined;
  error.value = "";
  loading.value = true;
  try {
    if (!rankKey.value) return;
    const r = await api<{ items: { rank: number; data: unknown }[] }>(
      `/servers/${encodeURIComponent(srv.value)}/modules/${encodeURIComponent(rankKey.value)}/rank?limit=200`
    );
    if (active && token === seq) rank.value = r;
  } catch (e) {
    if (active && token === seq) error.value = e instanceof ApiError ? gameError(e.status, e.message) : errorMessage(e);
  } finally {
    if (active && token === seq) loading.value = false;
  }
}
async function saveWorld() {
  let body;
  try {
    body = worldPayload(override.value, until.value, why.value);
  } catch (e) {
    error.value = errorMessage(e);
    return;
  }
  error.value = ""; // a validation error from an earlier attempt must not stay beside a valid one
  const server = srv.value;
  const r = await execute(
    () =>
      api<{ world_level: unknown; pushed: number; push_errors?: number }>(`/servers/${encodeURIComponent(server)}/worldlev`, {
        method: "PUT",
        body
      }),
    `设置 ${server} 世界等级覆盖为 ${body.override}，到期 ${until.value || "长期"}？原因：${body.reason}`
  );
  if (r && active) {
    ElMessage({
      type: r.push_errors ? "warning" : "success",
      message: `世界等级已保存，网关通知 ${r.pushed ?? 0} 个在线角色${r.push_errors ? "，部分通知失败" : ""}`
    });
    await load();
  }
}
async function settle() {
  let body;
  try {
    body = { reason: reason(settleReason.value) };
  } catch (e) {
    error.value = errorMessage(e);
    return;
  }
  error.value = "";
  const server = srv.value,
    key = settleTarget.value;
  const r = await execute(
    () =>
      api<{ settled: boolean; reason: string; season: Season }>(
        `/servers/${encodeURIComponent(server)}/seasons/${encodeURIComponent(key)}/settle`,
        { method: "POST", body }
      ),
    `再次确认补结算 ${server} / ${key}？原因：${body.reason}`
  );
  if (r && active) {
    ElMessage.success(settleFeedback(r));
    settleTarget.value = "";
    await load();
  }
}
function closeSettle(done: () => void) {
  if (!busy.value) done();
}
function selectGuild() {
  if (!/^[1-9][0-9]*$/.test(guildInput.value) || !Number.isSafeInteger(Number(guildInput.value))) {
    error.value = "请输入有效公会GID";
    return;
  }
  guildID.value = guildInput.value;
}
watch(srv, value => (selectedServer.value = value));
// Another address of this page (the same component serves them all): start from its first part.
watch(
  () => route.name,
  () => {
    tab.value = tabs.value[0].name;
    rankKey.value = "";
    globalKey.value = "";
    snapshots.value = [];
  }
);
watch([srv, tab], () => {
  settleTarget.value = "";
  seasonDetail.value = undefined;
  guildID.value = "";
  void load();
});
// The server and guild pages link here with ?srv= and, for a guild, &gid=. Only a listed server and a positive GID are taken.
async function applyRouteQuery() {
  const want = statusSelection(route.query, servers.value.map(s => s.srv_id));
  srv.value = want.srv || srv.value || servers.value[0]?.srv_id || "";
  if (!want.gid || section.value !== "tables") return;
  tab.value = "tables";
  await nextTick(); // the [srv, tab] watcher above has cleared the guild selection by now
  if (!active) return;
  tableScope.value = "guild";
  guildInput.value = want.gid;
  guildID.value = want.gid;
}
watch(
  () => route.query,
  () => {
    if (route.path.startsWith("/game-status") && servers.value.length) void applyRouteQuery();
  }
);
onMounted(async () => {
  try {
    const [r, ok] = await Promise.all([getServers(), catalog.load()]);
    if (!active) return;
    if (!ok) throw Error(catalog.error.value);
    servers.value = r.items;
    await applyRouteQuery();
  } catch (e) {
    if (active) error.value = errorMessage(e);
  }
});
onBeforeUnmount(() => {
  active = false;
  seq++;
});
</script>
