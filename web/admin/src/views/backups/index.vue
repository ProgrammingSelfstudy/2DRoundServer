<template>
  <CollectionPage
    title="数据库备份"
    :loading="loading"
    :error="error || commandError"
    hint="备份包含全部账号数据，请妥善保管。立即备份不会中断服务，文件只写入服务端配置的备份目录。"
    @refresh="load"
    ><template #actions
      ><el-button
        v-if="auth.hasPermission('manage')"
        type="primary"
        :loading="busy"
        :disabled="loading || !!error || uncertain"
        @click="make"
        >立即备份</el-button
      ></template
    >
    <p v-if="!error">
      共 {{ rows.length }} 份，保留策略：{{ Number(extra.keep) > 0 ? `最多 ${extra.keep} 份，旧备份自动删除` : "不自动清理" }}。
    </p>
    <el-alert v-if="result" :title="result" type="success" :closable="false" /><el-alert
      v-if="uncertain"
      title="备份请求结果不明，请先刷新列表核对文件。"
      type="warning"
      :closable="false" /><el-button v-if="uncertain" @click="review">已核对文件，继续操作</el-button
    ><RecordTable :rows="rows" :columns="columns" :loading="loading" /><el-alert
      title="恢复需手动操作：停止所有相关服务，用备份文件覆盖数据库，并删除同名 -wal / -shm 文件，再启动服务。请按项目 README 的恢复步骤执行。此页不提供在线恢复操作。"
      type="info"
      :closable="false"
  /></CollectionPage>
</template>
<script setup lang="ts" name="backups">
import { onMounted, ref } from "vue";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useCollection } from "@/composables/useCollection";
import { useCommand } from "@/composables/useCommand";
import { formatBytes, type Backup } from "@/domain/management";
import CollectionPage from "@/components/Admin/CollectionPage.vue";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
const auth = useAuthStore(),
  result = ref("");
const { rows, loading, error, extra, load } = useCollection<Backup>("/backups", 200, false);
const { busy, uncertain, error: commandError, execute, reviewed } = useCommand("manage", true);
const columns: Column[] = [
  { key: "name", label: "文件名", width: 340 },
  { key: "size", label: "大小", format: formatBytes },
  { key: "mod_time", label: "时间", time: true, width: 200 }
];
async function make() {
  const r = await execute(
    () => api<Backup & { pruned: number }>("/backups", { method: "POST" }),
    "立即生成数据库备份？按保留策略自动删除旧备份。"
  );
  if (r) {
    result.value = `已备份 ${r.name}（${formatBytes(r.size)}），清理 ${r.pruned} 份旧备份`;
    void load();
  }
}
async function review() {
  if (await load()) reviewed();
}
onMounted(() => load());
</script>
