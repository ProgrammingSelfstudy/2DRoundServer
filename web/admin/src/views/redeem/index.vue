<template>
  <CollectionPage
    title="兑换码"
    :loading="loading"
    :error="error || optionsError || commandError"
    hint="玩家在游戏设置中输入，兑换码不分大小写，每个角色只能使用一次；奖励直接发到背包。0 次数上限表示不限，0 天表示长期有效。"
    @refresh="load"
    ><template #actions
      ><el-button v-if="auth.hasPermission('write')" type="primary" :disabled="!meta" @click="open"
        >新建兑换码</el-button
      ></template
    ><el-alert
      v-if="uncertain"
      title="创建结果不明，先刷新列表核对兑换码是否已存在。"
      type="warning"
      :closable="false" /><el-button v-if="uncertain" @click="review">已核对列表，继续操作</el-button
    ><RecordTable :rows="rows" :columns="columns" :loading="loading"
      ><template #attachments="{ row }">{{ rewardText(row) }}</template
      ><template v-if="auth.hasPermission('write')" #actions="{ row }"
        ><el-button v-if="auth.hasPermission('write')" type="danger" :disabled="busy || uncertain" @click="remove(row)"
          >删除</el-button
        ></template
      ></RecordTable
    ><el-dialog
      v-model="dialog"
      title="新建兑换码"
      width="min(850px,94vw)"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      ><el-form v-if="meta" label-position="top" :disabled="busy || uncertain" @submit.prevent="save"
        ><el-form-item label="兑换码"
          ><el-input v-model="form.code" maxlength="32" aria-label="兑换码" placeholder="4～32 位字母或数字" /></el-form-item
        ><el-form-item label="备注（最多 200 字节）"
          ><el-input v-model="form.note" maxlength="100" aria-label="兑换码备注" /></el-form-item
        ><el-form-item label="总次数上限（0 = 不限）"
          ><IntegerInput v-model="form.max_uses" :min="0" :max="1000000" aria-label="总次数上限" /></el-form-item
        ><el-form-item label="有效天数（0 = 长期，最多 3650 天）"
          ><IntegerInput v-model="form.days" :min="0" :max="3650" aria-label="有效天数" /></el-form-item
        ><el-form-item label="奖励"
          ><AttachmentEditor v-model="rewards" :assets="meta.assets" :max="20" :disabled="busy || uncertain" /></el-form-item
        ><el-button @click="dialog = false">取消</el-button
        ><el-button type="primary" :loading="busy" @click="save">创建</el-button></el-form
      ><el-alert v-if="commandError" :title="commandError" type="error" :closable="false" /></el-dialog
  ></CollectionPage>
</template>
<script setup lang="ts" name="redeem">
import { onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getMeta, searchItems, type Meta } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { redeemPayload, type Redeem } from "@/domain/management";
import { assetTitle, type AttachmentInput } from "@/domain/operations";
import { formatTime, errorMessage } from "@/utils/admin";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
import AttachmentEditor from "@/components/Admin/AttachmentEditor.vue";
import IntegerInput from "@/components/Admin/IntegerInput.vue";
const auth = useAuthStore(),
  meta = ref<Meta>(),
  names = ref<Record<number, string>>({}),
  optionsError = ref(""),
  dialog = ref(false),
  rewards = ref<AttachmentInput[]>([]),
  form = ref({ code: "", note: "", max_uses: 0, days: 0 });
const { rows, loading, error, load: fetchRows } = useCollection<Redeem>("/redeem-codes", 200, false);
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("write", true);
const columns: Column[] = [
  { key: "code", label: "兑换码", width: 180 },
  { key: "note", label: "备注" },
  { key: "attachments", label: "奖励", width: 250 },
  { key: "used", label: "已用 / 上限", format: (v, r) => `${v} / ${r.max_uses || "不限"}` },
  { key: "expires_at", label: "有效期", format: v => (v ? (v * 1000 < Date.now() ? "已过期 · " : "") + formatTime(v) : "长期") },
  { key: "created_at", label: "创建", time: true }
];
async function load() {
  const ok = await fetchRows();
  if (ok) {
    const ids = [...new Set(rows.value.flatMap(r => (r.attachments || []).filter(a => a.kind === "item").map(a => a.item_id)))];
    await Promise.all(
      ids.map(async id => {
        try {
          const r = await searchItems(String(id));
          const item = r.items.find(i => i.base_id === id);
          if (item) names.value[id] = item.name;
        } catch {
          /* numeric ID remains visible */
        }
      })
    );
  }
  return ok;
}
function rewardText(r: Redeem) {
  return (r.attachments || [])
    .map(
      a =>
        `${a.kind === "asset" ? assetTitle(meta.value?.assets.find(b => b.label === a.item_id)?.name || `货币 ${a.item_id}`) : names.value[a.item_id] || `物品 ${a.item_id}`}×${a.num}`
    )
    .join("，");
}
function open() {
  form.value = { code: "", note: "", max_uses: 0, days: 0 };
  rewards.value = [];
  dialog.value = true;
}
async function save() {
  const r = await execute(
    () => api("/redeem-codes", { method: "POST", body: redeemPayload(form.value, rewards.value, meta.value!.assets) }),
    "创建此兑换码及奖励？"
  );
  if (r) {
    dialog.value = false;
    ElMessage.success("兑换码已创建");
    void load();
  }
}
async function remove(r: Redeem) {
  const res = await execute(
    () => api(`/redeem-codes/${encodeURIComponent(r.code)}`, { method: "DELETE" }),
    `删除兑换码 ${r.code}？已兑换的玩家保留奖励。`
  );
  if (res) {
    ElMessage.success("已删除");
    void load();
  }
}
async function review() {
  if (await load()) reviewed();
}
onMounted(() => {
  void load();
  void getMeta()
    .then(r => (meta.value = r))
    .catch(e => (optionsError.value = errorMessage(e)));
});
</script>
