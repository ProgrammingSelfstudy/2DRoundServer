<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>邮件管理</h1>
      <el-button :loading="loading" @click="loadHistory">刷新历史</el-button>
    </div>
    <el-alert v-if="optionsError" :title="optionsError" type="error" :closable="false"
      ><el-button @click="loadOptions">重新加载选项</el-button></el-alert
    >
    <el-card v-if="auth.hasPermission('write')" shadow="never" class="compose">
      <template #header>写邮件</template>
      <el-form
        v-if="meta"
        key="mail-options-ready"
        label-position="top"
        :disabled="busy || uncertain || !meta"
        @submit.prevent="send"
      >
        <div class="form-grid">
          <el-form-item label="发送范围" label-for="mail-scope"
            ><el-select id="mail-scope" v-model="form.scope"
              ><el-option label="指定角色" value="direct" /><el-option label="广播邮件" value="broadcast" /></el-select
          ></el-form-item>
          <el-form-item :label="form.scope === 'direct' ? '查找角色的区服（可选）' : '广播区服'" label-for="mail-server"
            ><el-select id="mail-server" :empty-values="[null, undefined]" v-model="form.srv_id"
              ><el-option :label="form.scope === 'direct' ? '全部区服' : '全服广播'" value="" /><el-option
                v-for="s in servers"
                :key="s.srv_id"
                :label="`${s.name}（${s.srv_id}）`"
                :value="s.srv_id" /></el-select
          ></el-form-item>
        </div>
        <template v-if="form.scope === 'direct'">
          <el-form-item label="查找并添加收件角色"
            ><RoleLookup :srv-id="form.srv_id" label="查找收件角色" @select="addRole"
          /></el-form-item>
          <el-form-item label="收件角色 ID（最多 500 个，逗号或空格分隔）" label-for="mail-roles"
            ><el-input id="mail-roles" v-model="form.roles" type="textarea" :rows="2" placeholder="角色 ID，如 1, 2"
          /></el-form-item>
        </template>
        <el-alert
          v-else
          title="广播按符合条件的角色查看邮件时逐步投递；尚未投递的玩家不计入当前收件数。"
          type="info"
          :closable="false"
        />
        <div class="form-grid">
          <el-form-item label="标题" label-for="mail-subject"
            ><el-input id="mail-subject" v-model="form.subject" maxlength="60" show-word-limit
          /></el-form-item>
          <el-form-item label="发件人" label-for="mail-from"
            ><el-input id="mail-from" v-model="form.from_name" maxlength="20"
          /></el-form-item>
          <el-form-item label="类型" label-for="mail-type"
            ><el-select id="mail-type" v-model="form.type"
              ><el-option label="类型 1" :value="1" /><el-option label="类型 3" :value="3" /></el-select
          ></el-form-item>
          <el-form-item label="有效期方式" label-for="mail-expiry"
            ><el-select id="mail-expiry" v-model="form.expiry"
              ><el-option label="有效天数" value="days" /><el-option label="指定失效时间" value="date" /></el-select
          ></el-form-item>
          <el-form-item v-if="form.expiry === 'days'" label="有效天数（0 为永久）" label-for="mail-days"
            ><IntegerInput id="mail-days" v-model="form.expire_days" :min="0" :max="365" :precision="0"
          /></el-form-item>
          <el-form-item v-else label="失效时间（本机时区）" label-for="mail-expire-at"
            ><el-input id="mail-expire-at" v-model="form.expire_at" type="datetime-local" step="1"
          /></el-form-item>
        </div>
        <el-form-item label="正文" label-for="mail-content"
          ><el-input id="mail-content" v-model="form.content" type="textarea" :rows="4" maxlength="2000" show-word-limit
        /></el-form-item>
        <h3>货币与物品附件</h3>
        <AttachmentEditor v-model="rows" :assets="meta?.assets || []" :disabled="busy || uncertain" />
        <el-alert v-if="formError || commandError" :title="formError || commandError" type="error" :closable="false" />
        <el-button type="primary" native-type="submit" :loading="busy">发送邮件</el-button>
      </el-form>
      <el-alert
        v-if="uncertain"
        title="发送结果未确认，请勿直接重发"
        description="请求中断不代表服务端没有保存。请先刷新并核对邮件历史，再决定是否重新填写。"
        type="warning"
        :closable="false"
        ><el-button @click="review">已核对历史，重新填写</el-button></el-alert
      >
      <el-alert v-if="lastResult" :title="lastResult.message" :type="lastResult.type" :closable="false" show-icon />
    </el-card>
    <p v-else class="hint">当前账号仅可查看邮件历史，不能发送邮件。</p>
    <h2>邮件历史</h2>
    <p class="hint">收件数为已建立收件关系的角色数，已领取为已领取附件的角色数；广播收件数会逐步增加，不表示全服已经收到。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-table :data="history" v-loading="loading" border row-key="id">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column label="范围" min-width="130"
        ><template #default="{ row }">{{
          row.scope === "direct" ? "定向" : `广播 · ${row.srv_id || "全服"}`
        }}</template></el-table-column
      >
      <el-table-column label="标题 / 详情" min-width="170"
        ><template #default="{ row }"
          ><el-button link type="primary" @click="selected = row">{{ row.subject }}</el-button></template
        ></el-table-column
      >
      <el-table-column label="附件" min-width="210"
        ><template #default="{ row }">{{ attachmentText(row) }}</template></el-table-column
      >
      <el-table-column label="收件 / 已领" width="110"
        ><template #default="{ row }">{{ row.recipients }} / {{ row.claimed }}</template></el-table-column
      >
      <el-table-column label="有效期" min-width="170"
        ><template #default="{ row }">{{ row.expire_at ? formatTime(row.expire_at) : "永久" }}</template></el-table-column
      >
      <el-table-column label="发送人" min-width="150"
        ><template #default="{ row }">{{ row.from_name }}（{{ row.created_by }}）</template></el-table-column
      >
      <el-table-column label="时间" min-width="170"
        ><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column
      >
      <template #empty>暂无邮件历史</template>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      :page-size="15"
      :total="total"
      layout="total, prev, pager, next"
      @current-change="loadHistory"
    />
    <el-dialog :model-value="!!selected" title="邮件详情" width="min(700px, 94vw)" @close="selected = null">
      <template v-if="selected"
        ><h3>{{ selected.subject }}</h3>
        <p>邮件 #{{ selected.id }} · 类型 {{ selected.type }} · 发件人 {{ selected.from_name }}</p>
        <p>
          {{ selected.scope === "direct" ? "定向邮件" : `广播 · ${selected.srv_id || "全服"}` }} · 有效期
          {{ selected.expire_at ? formatTime(selected.expire_at) : "永久" }}
        </p>
        <p v-if="selected.scope === 'broadcast'" class="hint">
          广播按符合条件的角色查看邮件时逐步投递，当前收件数不代表全服人数。
        </p>
        <p class="mail-body">{{ selected.content || "（正文为空）" }}</p>
        <p>附件：{{ attachmentText(selected) }}</p>
        <p>已建立收件关系 {{ selected.recipients }}，已领取附件 {{ selected.claimed }}。</p></template
      >
      <template #footer><el-button @click="selected = null">关闭</el-button></template>
    </el-dialog>
  </div>
</template>
<script setup lang="ts" name="mail">
import IntegerInput from "@/components/Admin/IntegerInput.vue";
import { onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { api } from "@/auth/runtime";
import { getMeta, getServers, type Meta, type RoleRow, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import {
  mailPayload,
  mailFeedback,
  roleIDs,
  type MailForm,
  type MailRow,
  type MailReply,
  type AttachmentInput
} from "@/domain/operations";
import { errorMessage, formatTime } from "@/utils/admin";
import AttachmentEditor from "@/components/Admin/AttachmentEditor.vue";
import RoleLookup from "@/components/Admin/RoleLookup.vue";
const auth = useAuthStore();
const route = useRoute();
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("write", true);
const form = reactive<MailForm>({
  scope: "direct",
  roles: "",
  srv_id: "",
  subject: "",
  content: "",
  from_name: "GM",
  type: 1,
  expiry: "days",
  expire_days: 7,
  expire_at: ""
});
const rows = ref<AttachmentInput[]>([]);
const meta = ref<Meta | null>(null);
const servers = ref<ServerRow[]>([]);
const optionsError = ref("");
const history = ref<MailRow[]>([]);
const itemNames = ref<Record<number, string>>({});
const loading = ref(false);
const error = ref("");
const page = ref(1);
const total = ref(0);
const formError = ref("");
const selected = ref<MailRow | null>(null);
const lastResult = ref<ReturnType<typeof mailFeedback>>();
let active = true;
let sequence = 0;
async function loadOptions() {
  optionsError.value = "";
  try {
    const [m, s] = await Promise.all([getMeta(), getServers()]);
    if (active) {
      meta.value = m;
      servers.value = s.items;
    }
  } catch (e) {
    if (active) optionsError.value = `表单选项加载失败：${errorMessage(e)}`;
  }
}
async function loadHistory() {
  const seq = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const data = await api<{ items: MailRow[]; total: number; item_names: Record<number, string> }>(
      `/mails?limit=15&offset=${(page.value - 1) * 15}`
    );
    if (!active || seq !== sequence) return;
    history.value = data.items;
    total.value = data.total;
    itemNames.value = data.item_names;
  } catch (e) {
    if (active && seq === sequence) {
      history.value = [];
      error.value = errorMessage(e);
    }
  } finally {
    if (active && seq === sequence) loading.value = false;
  }
}
function attachmentText(mail: MailRow) {
  return (
    (mail.attachments || [])
      .map(
        a =>
          `${a.kind === "asset" ? meta.value?.assets.find(x => x.label === a.item_id)?.name || `货币 ${a.item_id}` : itemNames.value[a.item_id] || `物品 ${a.item_id}`} × ${a.num}`
      )
      .join("，") || "—"
  );
}
function addRole(role: RoleRow) {
  try {
    form.roles = [...new Set([...roleIDs(form.roles), role.rid])].join(", ");
  } catch (e) {
    formError.value = errorMessage(e);
  }
}
async function send() {
  formError.value = "";
  if (!meta.value) return;
  let body: ReturnType<typeof mailPayload>;
  try {
    body = mailPayload(form, rows.value, meta.value.assets);
  } catch (e) {
    formError.value = errorMessage(e);
    return;
  }
  const scope =
    "role_ids" in body
      ? `指定角色 ${body.role_ids.join(", ")}`
      : body.srv_id
        ? `区服 ${body.srv_id} 的符合条件角色`
        : "全服符合条件角色";
  const result = await execute(
    () => api<MailReply>("/mails", { method: "POST", body }),
    `确认向${scope}发送“${body.subject}”？附件 ${body.attachments.length} 项；有效期 ${"expire_at" in body ? formatTime(body.expire_at) : body.expire_days === 0 ? "永久" : `${body.expire_days} 天`}。附件随邮件领取。`
  );
  if (result && active) {
    lastResult.value = mailFeedback(result);
    form.subject = "";
    form.content = "";
    rows.value = [];
    page.value = 1;
    void loadHistory();
  }
}
async function review() {
  await loadHistory();
  if (!error.value) {
    reviewed();
    form.subject = "";
    form.content = "";
    rows.value = [];
  }
}
watch(
  () => [route.query.to, route.query.srv_id],
  () => {
    if (route.name !== "mail" || busy.value) return;
    form.roles = String(route.query.to || "");
    form.srv_id = String(route.query.srv_id || "");
    form.scope = "direct";
  },
  { immediate: true }
);
onMounted(() => {
  void loadOptions();
  void loadHistory();
});
onBeforeUnmount(() => {
  active = false;
  sequence++;
});
</script>
<style scoped lang="scss">
.compose {
  margin-bottom: 24px;
}
.compose .el-alert {
  margin-top: 16px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}
.mail-body {
  white-space: pre-wrap;
  line-height: 1.8;
}
@media (max-width: 650px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
