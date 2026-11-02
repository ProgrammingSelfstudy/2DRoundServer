<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>区服管理</h1>
      <div>
        <el-button :loading="loading" @click="reload">刷新列表</el-button>
        <el-button v-if="auth.hasPermission('manage')" type="primary" @click="edit()">新建区服</el-button>
      </div>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-table :data="rows" v-loading="loading" row-key="srv_id" border>
      <el-table-column prop="srv_id" label="区服 ID" min-width="120" />
      <el-table-column prop="name" label="名称" min-width="130" />
      <el-table-column label="地址" min-width="160"
        ><template #default="{ row }">{{ row.host }}:{{ row.port }}</template></el-table-column
      >
      <el-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><el-tag :type="row.status === 1 ? 'success' : 'warning'">{{ row.status === 1 ? "开放" : "维护中" }}</el-tag></template
        ></el-table-column
      >
      <el-table-column label="标记" min-width="115"
        ><template #default="{ row }"
          ><el-tag v-if="row.is_new">新服</el-tag> <el-tag v-if="row.is_recommend" type="success">推荐</el-tag></template
        ></el-table-column
      >
      <el-table-column label="分组 / 分区" width="110"
        ><template #default="{ row }">{{ row.group_id }} / {{ row.zone_id }}</template></el-table-column
      >
      <el-table-column label="开服时间" min-width="170"
        ><template #default="{ row }">{{ formatTime(row.open_time) }}</template></el-table-column
      >
      <el-table-column prop="main_srv_id" label="合服到" min-width="110" />
      <el-table-column label="玩法" width="100" fixed="right"
        ><template #default="{ row }"
          ><el-button link type="primary" @click="router.push(statusLocation(row.srv_id))">玩法状态</el-button></template
        ></el-table-column
      >
      <el-table-column v-if="auth.hasPermission('manage')" label="操作" width="245" fixed="right"
        ><template #default="{ row }">
          <el-button link type="primary" :disabled="busy" @click="edit(row)">编辑</el-button>
          <el-button link type="warning" :disabled="busy" @click="toggle(row)">{{
            row.status === 1 ? "设为维护" : "恢复开放"
          }}</el-button>
          <el-button link type="danger" :disabled="busy" @click="remove(row)">删除</el-button>
        </template></el-table-column
      >
      <template #empty>暂无区服</template>
    </el-table>
    <el-dialog
      v-model="dialog"
      :title="creating ? '新建区服' : `编辑 ${form.srv_id}`"
      width="min(760px, 94vw)"
      destroy-on-close
      :close-on-click-modal="false"
      :before-close="closeDialog"
    >
      <el-form
        v-if="auth.hasPermission('manage')"
        :disabled="busy"
        label-position="top"
        @submit.prevent="save"
        class="server-form"
      >
        <el-form-item label="区服 ID" label-for="server-id"
          ><el-input id="server-id" v-model="form.srv_id" :disabled="!creating || busy" placeholder="symlf_2"
        /></el-form-item>
        <el-form-item label="名称" label-for="server-name"
          ><el-input id="server-name" v-model="form.name" maxlength="32"
        /></el-form-item>
        <el-form-item label="平台" label-for="server-platform"
          ><el-input id="server-platform" v-model="form.platform" maxlength="16"
        /></el-form-item>
        <el-form-item label="主机" label-for="server-host"><el-input id="server-host" v-model="form.host" /></el-form-item>
        <el-form-item label="端口" label-for="server-port"
          ><el-input-number id="server-port" v-model="form.port" :min="1" :max="65535" :precision="0"
        /></el-form-item>
        <el-form-item label="分组" label-for="server-group"
          ><el-input-number id="server-group" v-model="form.group_id" :min="1" :precision="0"
        /></el-form-item>
        <el-form-item label="分区号" label-for="server-zone"
          ><el-input-number id="server-zone" v-model="form.zone_id" :min="1" :precision="0"
        /></el-form-item>
        <el-form-item label="状态" label-for="server-status"
          ><el-select id="server-status" v-model="form.status"
            ><el-option label="开放" :value="1" /><el-option label="维护中" :value="0" /></el-select
        ></el-form-item>
        <el-form-item label="WebSocket 地址" label-for="server-ws"
          ><el-input id="server-ws" v-model="form.ws_url" placeholder="小游戏 wss 地址，可空"
        /></el-form-item>
        <el-form-item label="合服到" label-for="server-main"
          ><el-input id="server-main" v-model="form.main_srv_id" placeholder="已存在的主服 ID，可空"
        /></el-form-item>
        <el-form-item label="开服时间（本机时区）" label-for="server-open"
          ><el-date-picker id="server-open" v-model="openDate" type="datetime" placeholder="未设置"
        /></el-form-item>
        <el-form-item label="标记"
          ><el-checkbox v-model="form.is_new">新服</el-checkbox
          ><el-checkbox v-model="form.is_recommend">推荐</el-checkbox></el-form-item
        >
      </el-form>
      <el-alert v-else title="权限已变更，当前账号无法修改区服" type="warning" :closable="false" />
      <template #footer
        ><el-button :disabled="busy" @click="dialog = false">取消</el-button
        ><el-button v-if="auth.hasPermission('manage')" type="primary" :loading="busy" @click="save"
          >保存区服</el-button
        ></template
      >
    </el-dialog>
  </div>
</template>
<script setup lang="ts" name="servers">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import { getServers, type ServerRow } from "@/api/admin";
import { useAuthStore } from "@/stores/modules/auth";
import { confirmAction, useMutation } from "@/composables/useMutation";
import { statusLocation } from "@/domain/gameAdmin";
import { errorMessage, formatTime, validateServer } from "@/utils/admin";
const auth = useAuthStore(),
  router = useRouter();
const { busy, run } = useMutation("manage");
const rows = ref<ServerRow[]>([]);
const loading = ref(false);
const error = ref("");
const dialog = ref(false);
const creating = ref(true);
function defaults(): ServerRow {
  return {
    srv_id: "",
    name: "",
    platform: "symlf",
    zone_id: 1,
    host: "127.0.0.1",
    port: 8800,
    ws_url: "",
    status: 1,
    is_new: false,
    is_recommend: false,
    open_time: 0,
    main_srv_id: "",
    group_id: 1
  };
}
// Whitelist fields: Go rejects extra response fields such as created_at in requests.
function payload(row: ServerRow): ServerRow {
  return Object.fromEntries(Object.keys(defaults()).map(key => [key, row[key as keyof ServerRow]])) as unknown as ServerRow;
}
const form = reactive(defaults());
const openDate = ref<Date | undefined>();
async function reload() {
  loading.value = true;
  error.value = "";
  try {
    rows.value = (await getServers()).items;
  } catch (e) {
    rows.value = [];
    error.value = errorMessage(e);
  } finally {
    loading.value = false;
  }
}
function edit(row?: ServerRow) {
  creating.value = !row;
  Object.assign(form, row ? payload(row) : defaults());
  openDate.value = form.open_time ? new Date(form.open_time * 1000) : undefined;
  dialog.value = true;
}
function closeDialog(done: () => void) {
  if (!busy.value) done();
}
async function save() {
  await run(async () => {
    const body = payload(form);
    body.srv_id = body.srv_id.trim();
    body.name = body.name.trim();
    body.host = body.host.trim();
    body.platform = body.platform.trim();
    body.ws_url = body.ws_url.trim();
    body.main_srv_id = body.main_srv_id.trim();
    body.open_time = openDate.value ? Math.floor(openDate.value.getTime() / 1000) : 0;
    validateServer(body);
    await api(creating.value ? "/servers" : `/servers/${encodeURIComponent(form.srv_id)}`, {
      method: creating.value ? "POST" : "PUT",
      body
    });
    dialog.value = false;
    ElMessage.success("区服已保存");
    await reload();
  });
}
async function toggle(row: ServerRow) {
  const status = row.status === 1 ? 0 : 1;
  if (
    !(await confirmAction(
      status === 0 ? `将 ${row.srv_id} 设为维护？玩家将无法登录该服，已在线玩家不受影响。` : `恢复 ${row.srv_id} 开放？`
    ))
  )
    return;
  await run(async () => {
    await api(`/servers/${encodeURIComponent(row.srv_id)}`, { method: "PUT", body: { ...payload(row), status } });
    ElMessage.success("区服状态已更新");
    await reload();
  });
}
async function remove(row: ServerRow) {
  if (!(await confirmAction(`删除区服 ${row.srv_id}？只有没有角色的区服才能删除。`, "删除区服"))) return;
  await run(async () => {
    await api(`/servers/${encodeURIComponent(row.srv_id)}`, { method: "DELETE" });
    ElMessage.success("区服已删除");
    await reload();
  });
}
onMounted(reload);
</script>
<style scoped lang="scss">
.server-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}
@media (max-width: 600px) {
  .server-form {
    grid-template-columns: 1fr;
  }
}
</style>
