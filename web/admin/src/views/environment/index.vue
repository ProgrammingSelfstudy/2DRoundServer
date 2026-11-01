<template>
  <div class="card environment">
    <h1>后台开发环境</h1>
    <p>Geeker-Admin 布局、Vue、TypeScript、Element Plus 和 Pinia 已就绪。</p>
    <el-alert title="已接入真实管理员登录和权限；本页用于检查服务连接。" type="info" :closable="false" />
    <el-descriptions title="本地联调" :column="1" border>
      <el-descriptions-item label="健康接口">GET /api/healthz</el-descriptions-item>
      <el-descriptions-item label="Go API">
        <el-tag :type="status === 'ok' ? 'success' : status === 'error' ? 'danger' : 'info'">{{ message }}</el-tag>
      </el-descriptions-item>
    </el-descriptions>
    <el-button type="primary" :loading="loading" @click="probe">检查连接</el-button>
    <p>旧管理后台继续由 Go 服务提供，新前端尚未接管生产静态资源。</p>
  </div>
</template>
<script setup lang="ts" name="environment">
import { onMounted, ref } from "vue";
import { checkHealth } from "@/api/health";
const loading = ref(false);
const status = ref<"idle" | "ok" | "error">("idle");
const message = ref("等待检查");
async function probe() {
  loading.value = true;
  try {
    await checkHealth();
    status.value = "ok";
    message.value = "连接正常";
  } catch (error) {
    status.value = "error";
    message.value = `连接失败：${error instanceof Error ? error.message : String(error)}`;
  } finally {
    loading.value = false;
  }
}
onMounted(probe);
</script>
<style scoped lang="scss">
.environment {
  box-sizing: border-box;
  min-height: 100%;
  h1 { margin-top: 0; }
  p { line-height: 1.8; }
  .el-descriptions { margin: 24px 0; }
}
</style>
