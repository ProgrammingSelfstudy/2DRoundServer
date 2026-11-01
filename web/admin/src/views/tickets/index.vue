<template>
  <CollectionPage
    title="客服工单"
    :loading="loading"
    :error="error || commandError"
    hint="最近 100 条工单。待回复筛选仅作用于这 100 条。回复后，玩家下次打开工单列表时可见。"
    @refresh="load"
    ><template #filters><el-checkbox v-model="onlyOpen">只看待回复</el-checkbox></template
    ><el-alert
      v-if="uncertain"
      :title="`工单 #${selected?.id} 回复结果不明，请先核对原工单历史。`"
      type="warning"
      :closable="false"
    /><el-button v-if="uncertain" @click="dialog = true">核对原工单</el-button
    ><RecordTable :rows="visibleRows" :columns="columns" :loading="loading"
      ><template #actions="{ row }"
        ><el-button @click="open(row)">{{ auth.hasPermission("write") ? "查看 / 回复" : "查看" }}</el-button></template
      ></RecordTable
    ><el-dialog
      v-model="dialog"
      :title="`工单 #${selected?.id}：${selected?.title || ''}`"
      width="min(820px,94vw)"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      ><template v-if="selected"
        ><p>
          角色 {{ selected.rid }}（{{ selected.srv_id }}） · 手机 {{ selected.phone || "—" }} · 邮箱 {{ selected.email || "—" }} ·
          机型 {{ selected.phone_info || "—" }}
        </p>
        <p>状态 {{ ticketStates[selected.state] || "未知" }} · 评分 {{ selected.score || "—" }}</p>
        <div class="thread">
          <template v-for="(question, index) in selected.questions" :key="index"
            ><p>
              <strong>玩家 {{ index + 1 }}：</strong>{{ question }}
            </p>
            <p v-if="selected.answers[index]"><strong>客服：</strong>{{ selected.answers[index] }}</p></template
          >
          <p v-for="(answer, index) in selected.answers.slice(selected.questions.length)" :key="`a${index}`">
            <strong>客服：</strong>{{ answer }}
          </p>
        </div>
        <el-form v-if="auth.hasPermission('write')" :disabled="busy || uncertain" @submit.prevent="send"
          ><el-form-item label="回复内容"
            ><el-input
              v-model="content"
              type="textarea"
              :rows="5"
              maxlength="1000"
              show-word-limit
              aria-label="回复内容" /></el-form-item
          ><el-button @click="dialog = false">取消</el-button
          ><el-button type="primary" :loading="busy" @click="send">发送回复</el-button></el-form
        ><el-alert
          v-if="uncertain"
          title="回复结果不明，请先刷新查看历史回复，确认后再继续。"
          type="warning"
          :closable="false"
        /><el-button v-if="uncertain" @click="review">刷新并核对历史</el-button></template
      ></el-dialog
    ></CollectionPage
  >
</template>
<script setup lang="ts" name="tickets">
import { computed, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { ticketAnswer, ticketStates, type Ticket } from "@/domain/management";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const auth = useAuthStore(),
  onlyOpen = ref(false),
  dialog = ref(false),
  selected = ref<Ticket>(),
  content = ref("");
const { rows, loading, error, load } = useCollection<Ticket>("/tickets", 100, false);
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("write", true);
const visibleRows = computed(() => (onlyOpen.value ? rows.value.filter(t => t.state === 1) : rows.value));
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "rid", label: "角色" },
  { key: "srv_id", label: "区服" },
  { key: "issue_type", label: "类型" },
  { key: "title", label: "标题", width: 220 },
  { key: "state", label: "状态", format: v => ticketStates[v] || "未知" },
  { key: "score", label: "评分", format: v => v || "—" },
  { key: "created_at", label: "提交", time: true },
  { key: "answered_at", label: "回复", time: true }
];
function open(t: Ticket) {
  if (uncertain.value && selected.value) {
    dialog.value = true;
    return;
  }
  selected.value = t;
  content.value = "";
  dialog.value = true;
}
async function send() {
  if (!selected.value) return;
  const r = await execute(
    () => api(`/tickets/${selected.value!.id}/reply`, { method: "POST", body: ticketAnswer(content.value) }),
    "发送这条客服回复？"
  );
  if (r) {
    dialog.value = false;
    content.value = "";
    ElMessage.success("已回复；玩家下次打开工单列表可见");
    void load();
  }
}
async function review() {
  if (await load()) {
    selected.value = rows.value.find(t => t.id === selected.value?.id);
    reviewed();
  }
}
onMounted(() => load());
</script>
<style scoped>
.thread {
  white-space: pre-wrap;
  padding: 16px;
  background: var(--el-fill-color-light);
  margin: 16px 0;
}
</style>
