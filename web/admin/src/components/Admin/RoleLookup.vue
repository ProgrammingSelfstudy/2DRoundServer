<template>
  <div>
    <el-select
      v-model="selected"
      filterable
      remote
      :remote-method="search"
      :loading="loading"
      :aria-label="label"
      placeholder="按角色名、账号或 ID 查找"
      @change="choose"
      class="role-lookup"
    >
      <el-option
        v-for="role in choices"
        :key="role.rid"
        :value="role.rid"
        :label="`${role.name} · ${role.account}（${role.rid} / ${role.srv_id}）`"
      />
    </el-select>
    <span v-if="error" class="hint">{{ error }}</span>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { getRoles, type RoleRow } from "@/api/admin";
import { errorMessage } from "@/utils/admin";
const props = withDefaults(defineProps<{ srvId?: string; label?: string }>(), { label: "查找角色" });
const emit = defineEmits<{ select: [role: RoleRow] }>();
const selected = ref<number>();
const choices = ref<RoleRow[]>([]);
const error = ref("");
const loading = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
let sequence = 0;
function search(q: string) {
  clearTimeout(timer);
  const seq = ++sequence;
  error.value = "";
  if (!q.trim()) {
    choices.value = [];
    loading.value = false;
    return;
  }
  loading.value = true;
  timer = setTimeout(async () => {
    try {
      const data = await getRoles(q.trim(), props.srvId || "", 20, 0);
      if (seq === sequence) choices.value = data.items;
    } catch (e) {
      if (seq === sequence) error.value = errorMessage(e);
    } finally {
      if (seq === sequence) loading.value = false;
    }
  }, 250);
}
function choose(rid: number) {
  const role = choices.value.find(r => r.rid === rid);
  if (role) emit("select", role);
  selected.value = undefined;
}
onBeforeUnmount(() => {
  sequence++;
  clearTimeout(timer);
});
</script>
<style scoped lang="scss">
.role-lookup {
  width: min(420px, 100%);
}
</style>
