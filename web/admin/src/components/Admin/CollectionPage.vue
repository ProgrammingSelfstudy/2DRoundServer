<template>
  <div class="management-page">
    <div class="page-toolbar">
      <h1>{{ title }}</h1>
      <div><el-button :loading="loading" @click="$emit('refresh')">刷新列表</el-button><slot name="actions" /></div>
    </div>
    <slot name="filters" />
    <p v-if="hint" class="hint">{{ hint }}</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" /><slot /><el-pagination
      v-if="paged"
      :current-page="page"
      :page-size="limit"
      :total="total"
      layout="total, prev, pager, next"
      @current-change="$emit('page', $event)"
    />
  </div>
</template>
<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string;
    loading: boolean;
    error?: string;
    hint?: string;
    paged?: boolean;
    page?: number;
    total?: number;
    limit?: number;
  }>(),
  { limit: 25, page: 1, total: 0 }
);
defineEmits<{ refresh: []; page: [value: number] }>();
</script>
