<template>
  <el-dialog
    :model-value="true"
    :title="detail ? `角色「${detail.role.name}」（${rid}）` : `角色详情（${rid}）`"
    width="min(1050px, 96vw)"
    destroy-on-close
    :close-on-click-modal="false"
    :before-close="close"
  >
    <div v-loading="loading" style="min-height: 160px">
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <template v-if="detail">
        <el-tabs v-model="detailTab" :before-leave="() => !busy && !gameLocked"><el-tab-pane label="基本资料与发放" name="base" /><el-tab-pane label="玩法与只读数据" name="game" /></el-tabs>
        <RoleGameData v-if="detailTab === 'game'" :rid="rid" @locked="gameLocked = $event" />
        <template v-else>
        <el-descriptions :column="3" border>
          <el-descriptions-item label="账号">{{ detail.account }}</el-descriptions-item
          ><el-descriptions-item label="区服">{{ detail.role.srv_id }}</el-descriptions-item>
          <el-descriptions-item label="在线状态"
            ><el-tag :type="detail.online === true ? 'success' : detail.online === false ? 'info' : 'warning'">{{
              detail.online === true ? "在线" : detail.online === false ? "离线" : "未知（网关未连接）"
            }}</el-tag></el-descriptions-item
          >
          <el-descriptions-item label="等级">{{ detail.role.lev }}</el-descriptions-item
          ><el-descriptions-item label="VIP">{{ detail.role.vip_lev || 0 }}</el-descriptions-item
          ><el-descriptions-item label="封禁">{{ detail.banned ? "已封禁" : "正常" }}</el-descriptions-item>
        </el-descriptions>
        <el-alert
          v-if="detail.online == null"
          title="网关不可用：在线状态未知，资源写入成功也不代表已通知客户端。"
          type="warning"
          :closable="false"
        />
        <el-form v-if="auth.hasPermission('write')" :disabled="busy" class="role-actions" @submit.prevent>
          <h3>等级与 VIP</h3>
          <div class="action-row">
            <label for="role-level">等级</label
            ><el-input-number
              :key="`level-${detail.role.lev}`"
              id="role-level"
              v-model="level"
              :min="1"
              :max="200"
              :precision="0"
            /><el-checkbox v-model="kickLevel">同时请求踢下线</el-checkbox
            ><el-button :loading="busy" @click="setLevel">设置等级</el-button>
          </div>
          <div class="action-row">
            <label for="role-vip">VIP</label
            ><el-input-number
              :key="`vip-${detail.role.vip_lev}`"
              id="role-vip"
              v-model="vip"
              :min="0"
              :max="30"
              :precision="0"
            /><el-checkbox v-model="kickVip">同时请求踢下线</el-checkbox
            ><el-button :loading="busy" @click="setVip">设置 VIP</el-button>
          </div>
          <p class="hint">等级和 VIP 由客户端重新登录时读取；勾选踢线仅表示请求，当前接口不返回实际踢线结果。</p>
          <el-alert v-if="metaError" :title="metaError" type="error" :closable="false" />
          <template v-if="meta">
            <h3>发放货币与道具</h3>
            <p class="hint">直接进入资产或物品账本，不走邮件；部分物品不在游戏背包显示。一次最多 20 项，每项 1 到 10 亿。</p>
            <div v-for="(entry, index) in grants" :key="entry.key" class="action-row grant-row">
              <el-select v-model="entry.kind" :aria-label="`资源 ${index + 1} 类型`" class="kind-select"
                ><el-option label="货币" value="asset" /><el-option label="物品" value="item"
              /></el-select>
              <el-select
                v-if="entry.kind === 'asset'"
                v-model="entry.asset"
                filterable
                :aria-label="`资源 ${index + 1} 货币`"
                class="choice-select"
                ><el-option
                  v-for="a in meta.assets"
                  :key="a.name"
                  :value="a.name"
                  :label="`${assetName(a.name)}（${a.name} / ${a.label}）`"
              /></el-select>
              <ItemPicker v-else v-model="entry.item_id" class="choice-select" />
              <el-input-number
                v-model="entry.num"
                :min="1"
                :max="1000000000"
                :precision="0"
                :aria-label="`资源 ${index + 1} 数量`"
              />
              <el-button @click="grants.splice(index, 1)" :disabled="busy">移除</el-button>
            </div>
            <div class="action-row">
              <el-button :disabled="grants.length >= 20 || busy" @click="addGrant">添加一项</el-button
              ><el-button type="primary" :loading="busy" @click="grantResources">发放资源</el-button>
            </div>
            <h3>发放英雄</h3>
            <p class="hint">新英雄为 1 级，按所选星级发放；不检查英雄格子上限，一次最多 20 名。</p>
            <div class="action-row">
              <el-select v-model="heroBid" filterable aria-label="英雄" class="choice-select" @change="selectHero"
                ><el-option
                  v-for="h in meta.heroes"
                  :key="h.bid"
                  :value="h.bid"
                  :label="`${h.name}（${h.bid}，${camps[h.camp] || '-'}，${h.init_star}星起）`"
              /></el-select>
              <label for="hero-star">星级</label
              ><el-input-number
                id="hero-star"
                v-model="heroStar"
                :min="chosenHero?.init_star || 1"
                :max="chosenHero?.max_star || 15"
                :precision="0"
              />
              <label for="hero-count">数量</label
              ><el-input-number id="hero-count" v-model="heroCount" :min="1" :max="20" :precision="0" />
              <el-button type="primary" :loading="busy" @click="grantHero">发放英雄</el-button>
            </div>
            <h3>发放皮肤和外观</h3>
            <p class="hint">天数 0 为永久，最多 3650 天。皮肤会尝试通知在线角色；外观在玩家重新打开面板时读取。</p>
            <div class="action-row">
              <el-select v-model="skinId" filterable aria-label="皮肤" class="choice-select"
                ><el-option v-for="s in meta.skins" :key="s.id" :value="s.id" :label="`${s.name}（${s.id}，英雄 ${s.bid}）`"
              /></el-select>
              <label for="skin-days">皮肤天数</label
              ><el-input-number id="skin-days" v-model="skinDays" :min="0" :max="3650" :precision="0" /><el-button
                type="primary"
                :loading="busy"
                @click="grantSkin"
                >发放皮肤</el-button
              >
            </div>
            <div class="action-row">
              <el-select v-model="lookKind" aria-label="外观类型" class="kind-select" @change="selectLook"
                ><el-option v-for="(label, kind) in lookKinds" :key="kind" :value="kind" :label="label"
              /></el-select>
              <el-select v-if="lookChoices.length" v-model="lookId" aria-label="外观" filterable class="choice-select"
                ><el-option v-for="l in lookChoices" :key="l.id" :value="l.id" :label="`${l.name}（${l.id}）`"
              /></el-select>
              <el-input-number v-else v-model="lookId" aria-label="外观编号" :min="1" :precision="0" placeholder="编号" />
              <label for="look-days">外观天数</label
              ><el-input-number id="look-days" v-model="lookDays" :min="0" :max="3650" :precision="0" /><el-button
                type="primary"
                :loading="busy"
                @click="grantLook"
                >发放外观</el-button
              >
            </div>
          </template>
        </el-form>
        <h3>资源账本</h3>
        <el-table :data="detail.assets" border max-height="300"
          ><el-table-column prop="label" label="资源 ID" /><el-table-column label="名称"
            ><template #default="{ row }">{{ assetName(row.name) }}（{{ row.name }}）</template></el-table-column
          ><el-table-column prop="val" label="数量"
        /></el-table>
        <h3>物品账本</h3>
        <el-table :data="detail.items" border max-height="300"
          ><el-table-column prop="base_id" label="物品 ID" /><el-table-column prop="name" label="名称" /><el-table-column
            prop="num"
            label="数量"
          /><el-table-column label="游戏背包"
            ><template #default="{ row }">{{ row.listed ? "显示" : "不显示" }}</template></el-table-column
          ></el-table
        >
        </template>
      </template>
    </div>
    <template #footer
      ><el-button :disabled="busy || loading || gameLocked" @click="reload">刷新详情</el-button
      ><el-button v-if="detail && auth.hasPermission('write')" type="danger" :disabled="gameLocked" :loading="busy" @click="kick">踢下线</el-button
      ><el-button :disabled="busy || gameLocked" @click="emit('close')">关闭</el-button></template
    >
  </el-dialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getMeta, getRole, type Meta, type RoleDetail, type PushReply } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { confirmAction, useMutation } from "@/composables/useMutation";
import { errorMessage, integer, pushFeedback } from "@/utils/admin";
import ItemPicker from "./ItemPicker.vue";
import RoleGameData from "./RoleGameData.vue";
const detailTab = ref("base"), gameLocked = ref(false);
const props = defineProps<{ rid: number }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const auth = useAuthStore();
const { busy, run } = useMutation();
const detail = ref<RoleDetail | null>(null);
const meta = ref<Meta | null>(null);
const loading = ref(false);
const error = ref("");
const metaError = ref("");
let active = true;
const level = ref<number | undefined>(1);
const vip = ref<number | undefined>(0);
const kickLevel = ref(false);
const kickVip = ref(false);
const heroBid = ref<number>();
const heroStar = ref<number | undefined>(5);
const heroCount = ref<number | undefined>(1);
const skinId = ref<number>();
const skinDays = ref<number | undefined>(0);
const lookKind = ref("avatar");
const lookId = ref<number>();
const lookDays = ref<number | undefined>(0);
const lookKinds: Record<string, string> = { face: "头像", backdrop: "背景", avatar: "头像框", bubble: "气泡", title: "称号" };
const camps: Record<number, string> = { 1: "水", 2: "火", 3: "风", 4: "光", 5: "暗" };
const assetNames: Record<string, string> = {
  coin: "金币",
  gold: "钻石",
  exp: "角色经验",
  hero_exp: "英雄经验",
  energy: "情报值",
  vip_exp: "VIP 经验",
  guild: "公会贡献",
  arena_cent: "竞技积分",
  friend_point: "友情点"
};
const assetName = (name: string) => assetNames[name] || name || "未知";
const chosenHero = computed(() => meta.value?.heroes.find(h => h.bid === heroBid.value));
const lookChoices = computed(() => meta.value?.looks.filter(l => l.kind === lookKind.value) || []);
function selectHero() {
  heroStar.value = chosenHero.value?.init_star || 1;
}
function selectLook() {
  lookId.value = lookChoices.value[0]?.id;
}
interface GrantRow {
  key: number;
  kind: "asset" | "item";
  asset: string;
  item_id?: number;
  num?: number;
}
let grantKey = 0;
const grants = ref<GrantRow[]>([]);
function addGrant() {
  if (grants.value.length < 20) grants.value.push({ key: ++grantKey, kind: "asset", asset: "gold", num: 1 });
}
async function reload() {
  loading.value = true;
  error.value = "";
  try {
    const data = await getRole(props.rid);
    if (!active) return;
    detail.value = data;
    level.value = data.role.lev;
    vip.value = data.role.vip_lev || 0;
  } catch (e) {
    if (active) {
      detail.value = null;
      error.value = errorMessage(e);
    }
  } finally {
    if (active) loading.value = false;
  }
}
function close(done: () => void) {
  if (!busy.value && !gameLocked.value) {
    done();
    emit("close");
  }
}
async function write(action: string, body: unknown) {
  return api<PushReply>(`/roles/${props.rid}/${action}`, { method: "POST", body });
}
async function updated() {
  await reload();
  emit("changed");
}
async function setLevel() {
  if (!(await confirmAction(`设置角色 ${props.rid} 的等级为 ${level.value}？`))) return;
  await run(async () => {
    integer(level.value, 1, 200, "等级");
    await write("level", { lev: level.value, kick: kickLevel.value });
    ElMessage({
      type: kickLevel.value ? "warning" : "success",
      message: `等级已保存；${kickLevel.value ? "已请求踢线，结果需在网关确认" : "客户端重新登录后生效"}`
    });
    await updated();
  });
}
async function setVip() {
  if (!(await confirmAction(`设置角色 ${props.rid} 的 VIP 为 ${vip.value}？`))) return;
  await run(async () => {
    integer(vip.value, 0, 30, "VIP");
    await write("vip", { vip_lev: vip.value, kick: kickVip.value });
    ElMessage({
      type: kickVip.value ? "warning" : "success",
      message: `VIP 已保存；${kickVip.value ? "已请求踢线，结果需在网关确认" : "客户端重新登录后生效"}`
    });
    await updated();
  });
}
async function grantResources() {
  if (!grants.value.length) {
    ElMessage.error("请至少添加一项资源");
    return;
  }
  if (!(await confirmAction(`向角色 ${props.rid} 直接发放 ${grants.value.length} 项资源？`))) return;
  await run(async () => {
    const body = grants.value.map(g => {
      const num = integer(g.num, 1, 1000000000, "数量");
      if (g.kind === "asset") {
        if (!meta.value?.assets.some(a => a.name === g.asset)) throw new Error("请选择货币");
        return { kind: "asset", asset: g.asset, num };
      }
      return { kind: "item", item_id: integer(g.item_id, 1, Number.MAX_SAFE_INTEGER, "物品 ID"), num };
    });
    const result = await write("grant", { grants: body });
    ElMessage({ ...pushFeedback(result, "资源"), duration: 7000 });
    await updated();
  });
}
async function grantHero() {
  if (!(await confirmAction(`向角色 ${props.rid} 发放 ${chosenHero.value?.name || ""} ${heroCount.value} 名？`))) return;
  await run(async () => {
    if (!chosenHero.value) throw new Error("请选择英雄");
    integer(heroStar.value, chosenHero.value.init_star, chosenHero.value.max_star, "英雄星级");
    integer(heroCount.value, 1, 20, "英雄数量");
    const result = await write("heroes", { heroes: [{ bid: heroBid.value, star: heroStar.value, num: heroCount.value }] });
    ElMessage({ ...pushFeedback(result, "英雄"), duration: 7000 });
    await updated();
  });
}
async function grantSkin() {
  if (!(await confirmAction(`向角色 ${props.rid} 发放所选皮肤（${skinDays.value === 0 ? "永久" : `${skinDays.value} 天`}）？`)))
    return;
  await run(async () => {
    if (!meta.value?.skins.some(s => s.id === skinId.value)) throw new Error("请选择皮肤");
    integer(skinDays.value, 0, 3650, "皮肤天数");
    const result = await write("skins", { skin_id: skinId.value, days: skinDays.value });
    ElMessage({ ...pushFeedback(result, "皮肤"), duration: 7000 });
    await updated();
  });
}
async function grantLook() {
  if (!(await confirmAction(`向角色 ${props.rid} 发放所选${lookKinds[lookKind.value]}？`))) return;
  await run(async () => {
    integer(lookId.value, 1, Number.MAX_SAFE_INTEGER, "外观编号");
    integer(lookDays.value, 0, 3650, "外观天数");
    await write("looks", { kind: lookKind.value, base_id: lookId.value, days: lookDays.value });
    ElMessage.success("外观已保存，玩家重新打开对应面板时可见");
    await updated();
  });
}
async function kick() {
  if (!(await confirmAction(`将角色 ${props.rid} 踢下线？`, "踢下线"))) return;
  await run(async () => {
    const result = await api<{ kicked: boolean }>(`/roles/${props.rid}/kick`, { method: "POST", body: {} });
    ElMessage.success(result.kicked ? "已踢下线" : "角色当前不在线");
    await updated();
  });
}
onMounted(async () => {
  addGrant();
  await Promise.all([
    reload(),
    (async () => {
      try {
        const data = await getMeta();
        if (!active) return;
        meta.value = data;
        heroBid.value = data.heroes[0]?.bid;
        selectHero();
        skinId.value = data.skins[0]?.id;
        selectLook();
      } catch (e) {
        if (active) metaError.value = `资源选项加载失败：${errorMessage(e)}`;
      }
    })()
  ]);
});
onBeforeUnmount(() => {
  active = false;
});
</script>
<style scoped lang="scss">
.role-actions {
  padding: 4px 0 12px;
}
.action-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin: 12px 0;
}
.kind-select {
  width: 110px;
}
.choice-select {
  width: 310px;
}
.el-alert {
  margin-top: 16px;
}
</style>
