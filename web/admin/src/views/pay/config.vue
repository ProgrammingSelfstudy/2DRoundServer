<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>收款配置</h1>
      <el-button :loading="loading" @click="loadConfig">刷新</el-button>
    </div>
    <el-alert
      title="人工收款确认"
      description="玩家扫这里设置的收款码付款；到账要在“订单管理”里核对收款后手动确认。本页面不会接入或发起第三方支付。"
      type="info"
      :closable="false"
    />
    <el-alert v-if="configError" :title="configError" type="error" :closable="false" />
    <el-form label-position="top" :disabled="configBusy || !configReady" @submit.prevent="saveNotice">
      <div class="qr-grid">
        <el-card v-for="channel in channels" :key="channel.key" shadow="never">
          <template #header>{{ channel.label }}收款码</template>
          <img
            v-if="isSafeImage(config[channel.key])"
            :src="config[channel.key]"
            :alt="`${channel.label}收款码`"
            class="qr-image"
          />
          <p v-else class="hint">尚未设置收款码</p>
          <template v-if="manage">
            <label :for="channel.key">选择{{ channel.label }}图片（最多 600 KB）</label>
            <input
              :id="channel.key"
              type="file"
              accept="image/png,image/jpeg,image/webp"
              :disabled="configBusy || !configReady"
              @change="selectFile($event, channel.key)"
            />
            <div class="actions">
              <el-button type="primary" :loading="configBusy" @click="saveQr(channel.key)"
                >保存{{ channel.label }}收款码</el-button
              ><el-button v-if="config[channel.key]" type="danger" :disabled="configBusy" @click="clearQr(channel.key)"
                >清除{{ channel.label }}收款码</el-button
              >
            </div>
          </template>
        </el-card>
      </div>
      <el-form-item label="付款页提示（最多 300 字）" label-for="pay-notice"
        ><el-input id="pay-notice" v-model="notice" :readonly="!manage" type="textarea" :rows="3" maxlength="300" show-word-limit
      /></el-form-item>
      <el-button v-if="manage" type="primary" native-type="submit" :loading="configBusy">保存支付说明</el-button>
    </el-form>
    <el-alert v-if="configCommand.error.value" :title="configCommand.error.value" type="error" :closable="false" />
    <p v-if="!manage" class="hint">当前账号仅可查看收款配置；修改仅限超级管理员。</p>
  </div>
</template>
<script setup lang="ts" name="pay-config">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { validateQr } from "@/domain/operations";
import { isSafeImage } from "@/domain/payPages";
import { errorMessage } from "@/utils/admin";
const auth = useAuthStore();
const manage = computed(() => auth.hasPermission("manage"));
const configCommand = useCommand("manage");
const { busy: configBusy } = configCommand;
type Channel = "qr_wechat" | "qr_alipay";
const channels: { key: Channel; label: string }[] = [
  { key: "qr_wechat", label: "微信" },
  { key: "qr_alipay", label: "支付宝" }
];
const config = reactive({ qr_wechat: "", qr_alipay: "", notice: "" });
const files = reactive<Partial<Record<Channel, File>>>({});
const configReady = ref(false);
const configError = ref("");
const notice = ref("");
const loading = ref(false);
let active = true;
async function loadConfig() {
  configError.value = "";
  loading.value = true;
  try {
    const data = await api<typeof config>("/pay/config");
    if (active) {
      Object.assign(config, data);
      notice.value = data.notice;
      configReady.value = true;
    }
  } catch (e) {
    if (active) configError.value = errorMessage(e);
  } finally {
    if (active) loading.value = false;
  }
}
function selectFile(event: Event, key: Channel) {
  files[key] = (event.target as HTMLInputElement).files?.[0];
}
function readImage(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(Error("读取图片失败"));
    reader.readAsDataURL(file);
  });
}
async function saveQr(key: Channel) {
  const file = files[key];
  try {
    if (!file) throw Error("请先选择收款码图片");
    validateQr(file);
  } catch (e) {
    configCommand.error.value = errorMessage(e);
    return;
  }
  const saved = await configCommand.execute(async () =>
    api("/pay/config", { method: "PUT", body: { key, value: await readImage(file!) } })
  );
  if (saved && active) {
    ElMessage.success("收款码已保存");
    void loadConfig();
  }
}
async function clearQr(key: Channel) {
  const saved = await configCommand.execute(
    () => api("/pay/config", { method: "PUT", body: { key, value: "" } }),
    `清除${key === "qr_wechat" ? "微信" : "支付宝"}收款码？`
  );
  if (saved && active) {
    ElMessage.success("收款码已清除");
    void loadConfig();
  }
}
async function saveNotice() {
  if ([...notice.value].length > 300) {
    configCommand.error.value = "支付说明最多 300 字";
    return;
  }
  const value = notice.value;
  const saved = await configCommand.execute(() => api("/pay/config", { method: "PUT", body: { key: "notice", value } }));
  if (saved && active) {
    config.notice = value;
    ElMessage.success("支付说明已保存");
  }
}
onMounted(loadConfig);
onBeforeUnmount(() => (active = false));
</script>
<style scoped lang="scss">
.qr-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  margin: 16px 0;
}
.qr-image {
  width: 160px;
  height: 160px;
  object-fit: contain;
  border: 1px solid var(--el-border-color);
}
label {
  display: block;
  margin: 12px 0;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 12px 0;
}
@media (max-width: 650px) {
  .qr-grid {
    grid-template-columns: 1fr;
  }
}
</style>
