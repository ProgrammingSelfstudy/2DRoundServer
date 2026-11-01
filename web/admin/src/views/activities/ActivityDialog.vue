<template>
  <el-dialog
    :model-value="true"
    :title="copy ? '复制活动' : row ? '编辑活动' : '新建活动'"
    width="min(980px, 96vw)"
    :close-on-click-modal="false"
    :before-close="close"
    destroy-on-close
  >
    <el-alert
      v-if="copy"
      title="复制会保留活动号、页签、时间和配置，作为新记录保存；请按需调整。同页签同时只展示最早开始项。"
      type="info"
      :closable="false"
    />
    <el-form v-if="auth.hasPermission('write')" label-position="top" :disabled="busy || uncertain" @submit.prevent="save">
      <div class="form-grid">
        <el-form-item label="区服" label-for="activity-server"
          ><el-select id="activity-server" :empty-values="[null, undefined]" v-model="form.srv_id"
            ><el-option label="全服" value="" /><el-option
              v-for="s in servers"
              :key="s.srv_id"
              :value="s.srv_id"
              :label="`${s.name}（${s.srv_id}）`" /></el-select
        ></el-form-item>
        <el-form-item label="活动号 camp_id" label-for="activity-camp"
          ><IntegerInput id="activity-camp" v-model="form.camp_id" :min="1" :precision="0"
        /></el-form-item>
        <el-form-item label="页签 bid" label-for="activity-bid"
          ><IntegerInput id="activity-bid" v-model="form.bid" :min="1" :precision="0"
        /></el-form-item>
        <el-form-item label="标题" label-for="activity-title"
          ><el-input id="activity-title" v-model="form.title" maxlength="40"
        /></el-form-item>
        <el-form-item label="面板类型 panel_type（1–145）" label-for="activity-panel"
          ><IntegerInput id="activity-panel" v-model="form.panel_type" :min="1" :max="145" :precision="0"
        /></el-form-item>
        <el-form-item label="排序" label-for="activity-sort"
          ><IntegerInput id="activity-sort" v-model="form.sort_val" :precision="0"
        /></el-form-item>
        <el-form-item label="开始时间（本机时区）" label-for="activity-start"
          ><el-input id="activity-start" v-model="form.start" type="datetime-local" step="1"
        /></el-form-item>
        <el-form-item label="结束时间（留空为不结束）" label-for="activity-end"
          ><el-input id="activity-end" v-model="form.end" type="datetime-local" step="1"
        /></el-form-item>
        <el-form-item label="主界面图标 cli_type（0 为不挂图标）" label-for="activity-cli"
          ><IntegerInput id="activity-cli" v-model="form.cli_type" :precision="0"
        /></el-form-item>
        <el-form-item label="图标类型 type_ico" label-for="activity-type-ico"
          ><IntegerInput id="activity-type-ico" v-model="form.type_ico" :precision="0"
        /></el-form-item>
        <el-form-item v-for="field in fields" :key="field.key" :label="field.label" :label-for="`activity-${field.key}`"
          ><el-input
            :id="`activity-${field.key}`"
            v-model="form.texts[field.key]"
            :type="field.key === 'rule_str' ? 'textarea' : 'text'"
            :rows="3"
            :maxlength="field.key === 'rule_str' ? 2000 : 200"
        /></el-form-item>
      </div>
      <p v-if="row?.start_at === 0" class="hint">原活动未限制开始时间，开始时间留空可保持此设置。</p>
      <div class="checks">
        <el-checkbox v-model="form.status">开启活动</el-checkbox><el-checkbox v-model="form.daily">每天重新计数</el-checkbox>
      </div>
      <p class="hint">有专门玩法的活动号需使用约定编号。目标列表常用面板 2、3、4、15、16；事件为空表示直接可领。</p>
      <el-form-item label="目标（每行一个，最多 60 项）" label-for="activity-aims"
        ><el-input
          id="activity-aims"
          v-model="form.aims"
          type="textarea"
          :rows="7"
          spellcheck="false"
          placeholder="1 | 每日登录 | evt_cli_loaded | 1 | 3:100,10034:2"
      /></el-form-item>
      <p class="hint">
        格式：编号 | 说明 | 事件 | 目标数 | 奖励编号:数量,奖励编号:数量。每项目标 1–10 个奖励，数量 1–10 亿；事件示例
        evt_cli_loaded、evt_recruit、evt_friend_present。
      </p>
      <div class="reward-helper">
        <ItemPicker v-model="rewardId" allow-currency aria-label="查找活动奖励" /><IntegerInput
          v-model="rewardNum"
          :min="1"
          :max="1000000000"
          :precision="0"
          aria-label="活动奖励数量"
        /><el-button @click="addAim">添加奖励目标示例</el-button>
      </div>
      <el-collapse
        ><el-collapse-item title="其他 spec 扩展配置（JSON，编辑/复制时保留）" name="extra"
          ><p class="hint">这里保留表单之外的原始字段。不要重复填写上面的字段；服务端不支持的字段会报错，不会自动删掉。</p>
          <el-input
            v-model="form.extra"
            aria-label="其他活动配置 JSON"
            type="textarea"
            :rows="6"
            spellcheck="false" /></el-collapse-item
      ></el-collapse>
      <el-alert v-if="formError || commandError" :title="formError || commandError" type="error" :closable="false" />
    </el-form>
    <el-alert v-else title="权限已变更，当前账号不能修改活动。" type="warning" :closable="false" />
    <el-alert v-if="uncertain" title="保存结果未确认，请先在活动列表核对，勿直接重复创建。" type="warning" :closable="false" />
    <template #footer
      ><el-button :disabled="busy" @click="emit('close')">{{ uncertain ? "关闭并核对列表" : "取消" }}</el-button
      ><el-button v-if="auth.hasPermission('write')" type="primary" :disabled="uncertain" :loading="busy" @click="save"
        >保存活动</el-button
      ></template
    >
  </el-dialog>
</template>
<script setup lang="ts">
import IntegerInput from "@/components/Admin/IntegerInput.vue";
import { onBeforeUnmount, reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/auth/runtime";
import type { ServerRow } from "@/api/admin";
import { activityForm, activityPayload, int, parseAims, type Activity } from "@/domain/operations";
import { useAuthStore } from "@/stores/modules/auth";
import { useCommand } from "@/composables/useCommand";
import { errorMessage } from "@/utils/admin";
import ItemPicker from "@/views/roles/ItemPicker.vue";
const props = defineProps<{ row?: Activity; copy: boolean; servers: ServerRow[] }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const auth = useAuthStore();
const form = reactive(activityForm(props.row));
const formError = ref("");
const rewardId = ref<number>();
const rewardNum = ref<number | undefined>(1);
const { busy, uncertain, error: commandError, execute } = useCommand("write", true);
let active = true;
const fields = [
  { key: "title2", label: "副标题" },
  { key: "cli_type_name", label: "主界面图标名称" },
  { key: "ico", label: "图标资源 ico" },
  { key: "top_banner", label: "顶部横幅 top_banner" },
  { key: "rule_str", label: "规则说明" },
  { key: "time_str", label: "时间说明" },
  { key: "aim_title", label: "目标标题" },
  { key: "reward_title", label: "奖励标题" },
  { key: "bottom_alert", label: "底部提示" },
  { key: "channel_ban", label: "渠道限制 channel_ban" }
];
function close(done: () => void) {
  if (!busy.value) {
    done();
    emit("close");
  }
}
function addAim() {
  try {
    const id = int(rewardId.value, 1, Number.MAX_SAFE_INTEGER, "奖励编号");
    const num = int(rewardNum.value, 1, 1000000000, "奖励数量");
    const aims = parseAims(form.aims);
    if (aims.length >= 60) throw Error("目标最多 60 项");
    const next = Math.max(0, ...aims.map(a => a.id)) + 1;
    form.aims += `${form.aims.trim() ? "\n" : ""}${next} | 奖励目标 | | 0 | ${id}:${num}`;
  } catch (e) {
    formError.value = errorMessage(e);
  }
}
async function save() {
  formError.value = "";
  let body: ReturnType<typeof activityPayload>;
  try {
    body = activityPayload(form, props.row);
  } catch (e) {
    formError.value = errorMessage(e);
    return;
  }
  const create = !props.row || props.copy;
  const saved = await execute(
    () => api<Activity>(create ? "/activities" : `/activities/${props.row!.id}`, { method: create ? "POST" : "PUT", body }),
    `${create ? "创建" : "更新"}活动“${body.spec.title}”？${body.status ? "活动将按配置时间开放。" : "此活动保持关闭。"}`
  );
  if (saved && active) {
    ElMessage.success("活动已保存；网关运行时会按自身刷新周期读取，不代表已通知在线玩家");
    emit("saved");
  }
}
onBeforeUnmount(() => {
  active = false;
});
</script>
<style scoped lang="scss">
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 20px;
}
.checks,
.reward-helper {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin: 16px 0;
  align-items: center;
}
.reward-helper .el-select {
  width: 310px;
}
.el-alert {
  margin-top: 16px;
}
@media (max-width: 650px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
