<template>
  <div>
    <div v-for="(row, index) in modelValue" :key="row.key" class="attachment-row">
      <el-select v-model="row.kind" :aria-label="`附件 ${index + 1} 类型`" class="kind"
        ><el-option label="货币" value="asset" /><el-option label="物品" value="item"
      /></el-select>
      <el-select v-if="row.kind === 'asset'" v-model="row.asset" filterable :aria-label="`附件 ${index + 1} 货币`" class="choice"
        ><el-option v-for="a in assets" :key="a.name" :value="a.name" :label="`${assetTitle(a.name)}（${a.name} / ${a.label}）`"
      /></el-select>
      <ItemPicker v-else v-model="row.item_id" :aria-label="`附件 ${index + 1} 物品`" class="choice" />
      <IntegerInput v-model="row.num" :min="1" :max="1000000000" :precision="0" :aria-label="`附件 ${index + 1} 数量`" />
      <el-button @click="remove(index)" :disabled="disabled">移除附件</el-button>
    </div>
    <el-button :disabled="disabled || modelValue.length >= max" @click="add">添加附件</el-button>
    <p class="hint">最多 {{ max }} 项，每项 1 到 10 亿。货币进入资产账本；部分物品不在游戏背包显示。</p>
  </div>
</template>
<script setup lang="ts">
import IntegerInput from "@/components/Admin/IntegerInput.vue";
import { assetTitle, type AssetChoice, type AttachmentInput } from "@/domain/operations";
import ItemPicker from "@/views/roles/ItemPicker.vue";
const props = withDefaults(
  defineProps<{ modelValue: AttachmentInput[]; assets: AssetChoice[]; disabled?: boolean; max?: number }>(),
  { max: 8 }
);
const emit = defineEmits<{ "update:modelValue": [value: AttachmentInput[]] }>();
let sequence = 0;
function add() {
  if (props.modelValue.length < props.max)
    emit("update:modelValue", [
      ...props.modelValue,
      {
        key: (sequence = Math.max(sequence, 0, ...props.modelValue.map(row => row.key)) + 1),
        kind: "asset",
        asset: props.assets.some(a => a.name === "gold") ? "gold" : props.assets[0]?.name || "",
        num: 1
      }
    ]);
}
function remove(index: number) {
  emit(
    "update:modelValue",
    props.modelValue.filter((_, i) => i !== index)
  );
}
</script>
<style scoped lang="scss">
.attachment-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 12px;
  align-items: center;
}
.kind {
  width: 110px;
}
.choice {
  width: 300px;
}
</style>
