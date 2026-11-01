<template>
  <pre class="json-view">{{ pretty(value) }}</pre>
</template>
<script setup lang="ts">
import { pretty } from "@/domain/gameAdmin";
defineProps<{ value: unknown }>();
</script>
<style scoped>
.json-view {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 420px;
  overflow: auto;
  padding: 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}
</style>
