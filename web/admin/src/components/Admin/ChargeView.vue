<template>
  <template v-if="presentation">
    <RecordTable :rows="presentation.rows" :columns="presentation.columns" :loading="false" max-height="400" />
    <el-collapse
      ><el-collapse-item title="计算状态与协议字段"><JsonView :value="state.view" /></el-collapse-item
    ></el-collapse>
  </template>
  <JsonView v-else :value="state.view" />
</template>
<script setup lang="ts">
import { computed } from "vue";
import { chargePresentation, type ModuleState } from "@/domain/gameAdmin";
import RecordTable from "./RecordTable.vue";
import JsonView from "./JsonView.vue";
const props = defineProps<{ state: ModuleState }>();
const presentation = computed(() => chargePresentation(props.state));
</script>
