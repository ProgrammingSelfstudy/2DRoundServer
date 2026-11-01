<template>
  <div class="dashboard-page">
    <div class="page-toolbar">
      <h1>概览</h1>
      <el-button :loading="loading" @click="refresh">刷新统计</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div class="stat-grid" v-loading="loading">
      <el-card v-for="card in cards" :key="card.label" shadow="never">
        <div class="stat-label">{{ card.label }}</div>
        <strong>{{ card.value }}</strong>
      </el-card>
    </div>
    <el-alert
      v-if="online?.online == null && !loading"
      title="在线人数暂不可用"
      description="网关未连接或健康检查失败；数据库统计仍可查看，不代表在线人数为零。"
      type="warning"
      :closable="false"
      show-icon
    />
    <p class="hint">统计来自当前数据库，在线人数来自网关。{{ checkedAt ? `最近检查：${checkedAt}` : "" }}</p>
    <el-card v-if="auth.hasPermission('write')" shadow="never">
      <template #header>运营广播</template>
      <form @submit.prevent="broadcast">
        <el-form-item label="广播内容" label-for="broadcast-message">
          <el-input
            id="broadcast-message"
            v-model="message"
            placeholder="向所有在线玩家滚动播放，最多 100 字"
            maxlength="100"
            show-word-limit
            :disabled="busy"
          />
        </el-form-item>
        <el-button type="primary" native-type="submit" :loading="busy">发送广播</el-button>
      </form>
      <p class="hint">广播只发送给在线玩家，不进入聊天记录；网关不可用时不会显示发送成功。</p>
    </el-card>
  </div>
</template>
<script setup lang="ts" name="dashboard">
import { computed, onMounted, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { confirmAction, useMutation } from "@/composables/useMutation";
import { errorMessage } from "@/utils/admin";
const auth = useAuthStore();
const { busy, run } = useMutation();
type Stats = Record<"accounts" | "roles" | "servers" | "mails" | "notices", number>;
const stats = ref<Stats | null>(null);
const online = ref<{ online: number | null; error?: string } | null>(null);
const loading = ref(false);
const error = ref("");
const checkedAt = ref("");
const message = ref("");
const format = (value: number | undefined | null) => (value == null ? "—" : value.toLocaleString("zh-CN"));
const cards = computed(() => [
  { label: "在线角色", value: format(online.value?.online) },
  ...(
    [
      ["accounts", "账号"],
      ["roles", "角色"],
      ["servers", "区服"],
      ["mails", "已发邮件"],
      ["notices", "公告"]
    ] as const
  ).map(([key, label]) => ({ label, value: format(stats.value?.[key]) }))
]);
async function refresh() {
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  stats.value = null;
  online.value = null;
  const [s, o] = await Promise.allSettled([api<Stats>("/stats"), api<{ online: number | null; error?: string }>("/online")]);
  if (s.status === "fulfilled") stats.value = s.value;
  else error.value = `数据库统计加载失败：${errorMessage(s.reason)}`;
  if (o.status === "fulfilled") online.value = o.value;
  checkedAt.value = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  loading.value = false;
}
async function broadcast() {
  const msg = message.value.trim();
  if (!msg || [...msg].length > 100 || /\p{Cc}/u.test(msg)) {
    ElMessage.error("广播需为 1 到 100 字，不能包含控制字符");
    return;
  }
  if (!(await confirmAction(`向所有在线玩家发送广播：“${msg}”？`, "发送运营广播"))) return;
  await run(async () => {
    const result = await api<{ pushed: number }>("/broadcast", { method: "POST", body: { msg } });
    ElMessage.success(`广播已发送给 ${result.pushed} 个在线角色`);
    message.value = "";
  });
}
onMounted(refresh);
</script>
<style scoped lang="scss">
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
  margin: 20px 0;
}
.stat-label {
  margin-bottom: 12px;
  color: var(--el-text-color-secondary);
}
strong {
  font-size: 32px;
}
</style>
