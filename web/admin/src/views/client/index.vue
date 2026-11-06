<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>客户端资源与版本</h1>
      <el-select v-model="platform" aria-label="平台" style="width: 140px" @change="load">
        <el-option v-for="p in platforms" :key="p.value" :label="p.label" :value="p.value" />
      </el-select>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-alert
      title="新客户端启动时向 /client/config 取这里的内容"
      description="“全部平台”的设置是默认值，选了具体平台后，该平台自己填的值优先。发布版本前先在草稿上设测试白名单，确认无误再清空白名单面向所有人。"
      type="info"
      :closable="false"
    />
    <el-alert v-if="error || command.error.value" :title="error || command.error.value" type="error" :closable="false" />

    <h2>启动设置</h2>
    <el-form label-position="top" :disabled="!manage || command.busy.value">
      <el-form-item v-for="f in fields" :key="f.key" :label="f.label">
        <el-input v-model="draft[f.key]" :aria-label="f.label" :placeholder="f.hint" maxlength="500" @change="saveSetting(f.key)" />
      </el-form-item>
      <el-form-item label="功能开关（每行 名称=值，名称只能用小写字母、数字和下划线）">
        <el-input v-model="switchText" type="textarea" :rows="4" aria-label="功能开关" />
      </el-form-item>
      <el-button v-if="manage" type="primary" :loading="command.busy.value" @click="saveSwitches">保存功能开关</el-button>
    </el-form>

    <h2>资源版本</h2>
    <RecordTable :rows="releases" :columns="columns" :loading="loading"
      ><template #actions="{ row }"
        ><template v-if="manage"
          ><el-button v-if="row.status !== 1" @click="setState(row, 1)">发布</el-button
          ><el-button v-if="row.status === 1" type="danger" @click="setState(row, 2)">回滚</el-button
          ><el-button @click="editWhitelist(row)">白名单</el-button></template
        ></template
      ></RecordTable
    >
    <el-form v-if="manage && platform !== '*'" inline class="create" @submit.prevent="create">
      <el-form-item label="版本"><el-input v-model="form.version" aria-label="版本" /></el-form-item>
      <el-form-item label="清单地址"
        ><el-input v-model="form.manifest_url" aria-label="清单地址" style="width: 320px" placeholder="https://..."
      /></el-form-item>
      <el-form-item label="清单 hash"><el-input v-model="form.manifest_hash" aria-label="清单 hash" /></el-form-item>
      <el-form-item label="备注"><el-input v-model="form.note" aria-label="备注" maxlength="200" /></el-form-item>
      <el-button type="primary" native-type="submit" :loading="command.busy.value">登记草稿</el-button>
    </el-form>
    <p v-else-if="manage" class="hint">登记资源版本要先在右上角选一个具体平台。</p>
    <p v-if="!manage" class="hint">当前账号仅可查看；修改仅限超级管理员。</p>
  </div>
</template>
<script setup lang="ts" name="client">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { api } from "@/auth/runtime";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { errorMessage } from "@/utils/admin";
import RecordTable, { type Column } from "@/components/Admin/RecordTable.vue";
interface Release {
  id: number;
  platform: string;
  version: string;
  manifest_url: string;
  manifest_hash: string;
  status: number;
  whitelist: string;
  note: string;
  created_at: number;
  published_at: number;
}
const platforms = [
  { value: "*", label: "全部平台" },
  { value: "wechat", label: "微信小游戏" },
  { value: "douyin", label: "抖音小游戏" },
  { value: "h5", label: "网页 H5" },
  { value: "android", label: "Android" },
  { value: "ios", label: "iOS" }
];
const fields = [
  { key: "cdn_base", label: "资源 CDN 地址", hint: "https://cdn.example.com/res" },
  { key: "update_url", label: "App 下载页地址", hint: "强制更新时跳转到这里" },
  { key: "min_version", label: "最低客户端版本", hint: "低于此版本会被要求更新，如 1.2.0" },
  { key: "notice", label: "启动公告", hint: "留空则不显示" }
] as const;
const stateNames = ["草稿", "已发布", "已回滚"];
const auth = useAuthStore();
const manage = computed(() => auth.hasPermission("manage"));
const command = useCommand("manage");
const platform = ref("*");
const loading = ref(false);
const error = ref("");
const releases = ref<Release[]>([]);
const saved = ref<Record<string, string>>({});
const draft = reactive<Record<string, string>>({ cdn_base: "", update_url: "", min_version: "", notice: "" });
const switchText = ref("");
const form = reactive({ version: "", manifest_url: "", manifest_hash: "", note: "" });
const columns: Column[] = [
  { key: "id", label: "ID" },
  { key: "platform", label: "平台" },
  { key: "version", label: "版本" },
  { key: "manifest_url", label: "清单地址", width: 260 },
  { key: "status", label: "状态", format: v => stateNames[v] || "未知" },
  { key: "whitelist", label: "测试白名单", format: v => v || "所有人" },
  { key: "note", label: "备注" },
  { key: "published_at", label: "发布时间", time: true }
];
let active = true;
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const q = platform.value === "*" ? "" : `?platform=${platform.value}`;
    const data = await api<{ config: Record<string, string>; releases: Release[] }>(`/client${q}`);
    if (!active) return;
    releases.value = data.releases;
    saved.value = data.config;
    for (const f of fields) draft[f.key] = data.config[f.key] ?? "";
    switchText.value = Object.entries(data.config)
      .filter(([k]) => k.startsWith("switch."))
      .map(([k, v]) => `${k.slice(7)}=${v}`)
      .join("\n");
  } catch (e) {
    if (active) error.value = errorMessage(e);
  } finally {
    if (active) loading.value = false;
  }
}
const put = (key: string, value: string) =>
  api("/client/config", { method: "PUT", body: { platform: platform.value, key, value } });
async function saveSetting(key: string) {
  if (await command.execute(() => put(key, draft[key].trim()))) ElMessage.success("已保存");
}
async function saveSwitches() {
  const want: Record<string, string> = {};
  for (const line of switchText.value.split("\n").filter(l => l.trim())) {
    const i = line.indexOf("=");
    const name = line.slice(0, i).trim();
    if (i < 1 || !/^[a-z0-9_]{1,32}$/.test(name)) {
      command.error.value = `开关“${line}”格式不对，应为 名称=值`;
      return;
    }
    want[name] = line.slice(i + 1).trim();
  }
  const old = Object.keys(saved.value).filter(k => k.startsWith("switch.")).map(k => k.slice(7));
  const done = await command.execute(async () => {
    for (const [n, v] of Object.entries(want)) await put(`switch.${n}`, v);
    for (const n of old.filter(n => !(n in want))) await put(`switch.${n}`, "");
    return true;
  });
  if (done) {
    ElMessage.success("功能开关已保存");
    void load();
  }
}
async function create() {
  const done = await command.execute(
    () => api("/client/releases", { method: "POST", body: { ...form, platform: platform.value } }),
    `登记 ${platform.value} 版本 ${form.version}（草稿，不会立刻生效）？`
  );
  if (done) {
    Object.assign(form, { version: "", manifest_url: "", manifest_hash: "", note: "" });
    void load();
  }
}
async function setState(r: Release, status: number) {
  const done = await command.execute(
    () => api(`/client/releases/${r.id}`, { method: "PUT", body: { status } }),
    status === 1 ? `发布 ${r.platform} 版本 ${r.version}？${r.whitelist ? "" : "白名单为空，所有玩家都会收到。"}` : `回滚 ${r.platform} 版本 ${r.version}？`
  );
  if (done) void load();
}
async function editWhitelist(r: Release) {
  let value: string;
  try {
    ({ value } = await ElMessageBox.prompt("填账号名，用逗号隔开；留空表示所有人可见", `${r.version} 的测试白名单`, {
      inputValue: r.whitelist
    }));
  } catch {
    return;
  }
  const done = await command.execute(() =>
    api(`/client/releases/${r.id}`, { method: "PUT", body: { status: r.status, whitelist: value.trim() } })
  );
  if (done) void load();
}
onMounted(load);
onBeforeUnmount(() => (active = false));
</script>
<style scoped lang="scss">
h2 {
  margin: 24px 0 12px;
  font-size: 16px;
}
.create {
  margin-top: 16px;
}
</style>
