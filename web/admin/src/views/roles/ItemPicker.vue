<template>
  <div class="item-picker">
    <el-select
      :aria-label="String($attrs['aria-label'] || '物品名称或 ID')"
      :model-value="modelValue"
      filterable
      remote
      :remote-method="search"
      :loading="loading"
      :placeholder="'输入物品中文名或 ID'"
      @update:model-value="$emit('update:modelValue', $event)"
      clearable
    >
      <el-option
        v-for="item in choices"
        :key="item.base_id"
        :value="item.base_id"
        :label="`${item.name}（${item.base_id}）${item.in_backpack ? '' : ' · 不在背包显示'}`"
        :disabled="!allowCurrency && item.currency"
      />
    </el-select>
    <span v-if="error" class="hint">{{ error }}</span>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { searchItems, type ItemChoice } from "@/api/admin";
import { errorMessage } from "@/utils/admin";
withDefaults(defineProps<{ modelValue?: number; allowCurrency?: boolean }>(), { allowCurrency: false });
defineEmits<{ "update:modelValue": [value: number | undefined] }>();
const choices = ref<ItemChoice[]>([]);
const loading = ref(false);
const error = ref("");
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
      const result = await searchItems(q.trim());
      if (seq === sequence) choices.value = result.items;
    } catch (e) {
      if (seq === sequence) error.value = errorMessage(e);
    } finally {
      if (seq === sequence) loading.value = false;
    }
  }, 250);
}
onBeforeUnmount(() => {
  sequence++;
  clearTimeout(timer);
});
</script>

<style scoped lang="scss">
.item-picker {
  display: inline-flex;
  flex-direction: column;
  min-width: 0;
}
.item-picker .el-select {
  width: 100%;
}
</style>
