<template>
  <CollectionPage
    title="公告管理"
    :loading="loading"
    :error="error || optionsError || commandError"
    paged
    :page="page"
    :total="total"
    hint="游戏邮箱「公告」标签只显示：状态为显示、已开始、未结束且匹配区服的公告。保存/删除会尝试实时通知；自然过期的公告需玩家重新登录才消失。时间采用当前浏览器时区。"
    @refresh="load"
    @page="changePage"
    ><template #actions
      ><el-button v-if="auth.hasPermission('write')" type="primary" :disabled="busy || uncertain" @click="open()"
        >新建公告</el-button
      ></template
    ><el-alert v-if="feedback" :title="feedback.message" :type="feedback.type" :closable="false" /><el-alert
      v-if="uncertain"
      title="提交结果不明，请先刷新列表核对公告。"
      type="warning"
      :closable="false" /><el-button v-if="uncertain" @click="review">已核对列表，继续操作</el-button
    ><RecordTable :rows="rows" :columns="columns" :loading="loading" :action-width="220"
      ><template #actions="{ row }"
        ><el-button @click="detail = row">详情</el-button
        ><el-button v-if="auth.hasPermission('write')" :disabled="busy || uncertain" @click="open(row)">编辑</el-button
        ><el-button v-if="auth.hasPermission('write')" type="danger" :disabled="busy || uncertain" @click="remove(row)"
          >删除</el-button
        ></template
      ></RecordTable
    ><el-dialog :model-value="!!detail" title="公告内容" @close="detail = undefined"
      ><h3>{{ detail?.title }}</h3>
      <p style="white-space: pre-wrap">{{ detail?.content }}</p></el-dialog
    ><el-dialog
      v-model="dialog"
      :title="selected ? '编辑公告' : '新建公告'"
      width="min(800px,94vw)"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      ><el-form label-position="top" :disabled="busy || uncertain" @submit.prevent="save"
        ><el-form-item label="标题"
          ><el-input v-model="form.title" maxlength="100" show-word-limit aria-label="公告标题" /></el-form-item
        ><el-form-item label="区服"
          ><el-select v-model="form.srv_id" :empty-values="[null, undefined]" aria-label="公告区服"
            ><el-option label="全服" value="" /><el-option
              v-for="s in servers"
              :key="s.srv_id"
              :value="s.srv_id"
              :label="s.name + '（' + s.srv_id + '）'" /></el-select></el-form-item
        ><el-form-item label="内容"
          ><el-input
            v-model="form.content"
            type="textarea"
            :rows="7"
            maxlength="2000"
            show-word-limit
            aria-label="公告内容" /></el-form-item
        ><el-form-item label="开始时间（留空 = 立即，时间戳 0）"
          ><el-input v-model="form.start" type="datetime-local" step="1" aria-label="公告开始时间" /></el-form-item
        ><el-form-item label="结束时间（留空 = 不结束）"
          ><el-input v-model="form.end" type="datetime-local" step="1" aria-label="公告结束时间" /></el-form-item
        ><el-checkbox v-model="form.shown">显示</el-checkbox>
        <div style="margin-top: 20px">
          <el-button @click="dialog = false">取消</el-button
          ><el-button type="primary" :loading="busy" @click="save">保存</el-button>
        </div></el-form
      ><el-alert v-if="commandError" :title="commandError" type="error" :closable="false" /></el-dialog
  ></CollectionPage>
</template>
<script setup lang="ts" name="notices">
import { onMounted, ref } from "vue";
import { api } from "@/auth/runtime";
import { getServers, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { noticePayload, noticeFeedback, type Notice } from "@/domain/management";
import { localInput } from "@/domain/operations";
import { errorMessage } from "@/utils/admin";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const auth = useAuthStore(),
  servers = ref<ServerRow[]>([]),
  optionsError = ref(""),
  dialog = ref(false),
  selected = ref<Notice>(),
  detail = ref<Notice>(),
  feedback = ref<ReturnType<typeof noticeFeedback>>(),
  form = ref({ title: "", content: "", srv_id: "", start: "", end: "", shown: true });
const { rows, loading, error, page, total, load } = useCollection<Notice>("/notices");
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("write", true);
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "title", label: "标题" },
  { key: "content", label: "内容", width: 240 },
  { key: "srv_id", label: "区服", format: v => v || "全服" },
  { key: "start_at", label: "开始", time: true },
  { key: "end_at", label: "结束", format: v => (v ? new Date(v * 1000).toLocaleString("zh-CN", { hour12: false }) : "不结束") },
  { key: "status", label: "状态", format: v => (v === 1 ? "显示" : "隐藏") },
  { key: "created_by", label: "发布人" }
];
function changePage(v: number) {
  page.value = v;
  void load();
}
function open(n?: Notice) {
  selected.value = n;
  form.value = n
    ? {
        title: n.title,
        content: n.content,
        srv_id: n.srv_id,
        start: localInput(n.start_at),
        end: localInput(n.end_at),
        shown: n.status === 1
      }
    : { title: "", content: "", srv_id: "", start: "", end: "", shown: true };
  dialog.value = true;
}
async function save() {
  const r = await execute(
    () =>
      api<{ pushed: number | null; push_error?: string }>(selected.value ? `/notices/${selected.value.id}` : "/notices", {
        method: selected.value ? "PUT" : "POST",
        body: noticePayload(form.value)
      }),
    "保存此公告？会尝试通知匹配区服的在线角色。"
  );
  if (r) {
    dialog.value = false;
    feedback.value = noticeFeedback(r);
    void load();
  }
}
async function remove(n: Notice) {
  const r = await execute(
    () => api<{ pushed: number | null; push_error?: string }>(`/notices/${n.id}`, { method: "DELETE" }),
    `删除公告“${n.title}”？`
  );
  if (r) {
    feedback.value = noticeFeedback(r, true);
    void load();
  }
}
async function review() {
  if (await load()) reviewed();
}
onMounted(() => {
  void load();
  void getServers()
    .then(r => (servers.value = r.items))
    .catch(e => (optionsError.value = errorMessage(e)));
});
</script>
