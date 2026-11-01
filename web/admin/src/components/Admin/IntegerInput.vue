<template>
  <el-input
    class="integer-input"
    :model-value="modelValue ?? ''"
    type="number"
    :min="min"
    :max="max"
    step="1"
    :disabled="disabled"
    @update:model-value="update"
  />
</template>
<script setup lang="ts">
// Use the ordinary input's controlled value. Avoid stale display/ARIA state in
// the upstream InputNumber when disabled by a parent form or updated after a save.
defineProps<{ modelValue?: number; min?: number; max?: number; precision?: number; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: number | undefined] }>();
function update(value: string | number) {
  emit("update:modelValue", value === "" ? undefined : Number(value));
}
</script>
<style scoped lang="scss">
.integer-input {
  width: 180px;
}
</style>
