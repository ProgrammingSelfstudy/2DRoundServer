<template>
  <el-table :data="rows" v-loading="loading" border
    ><el-table-column
      v-for="c in columns"
      :key="c.key"
      :prop="c.key"
      :label="c.label"
      :min-width="c.width || 130"
      show-overflow-tooltip
      ><template #default="{ row }"
        ><slot :name="c.key" :row="row">{{
          c.time ? formatTime(row[c.key]) : c.format ? c.format(row[c.key], row) : (row[c.key] ?? "—")
        }}</slot></template
      ></el-table-column
    ><el-table-column v-if="$slots.actions" label="操作" :width="actionWidth" fixed="right"
      ><template #default="{ row }"><slot name="actions" :row="row" /></template></el-table-column
    ><template #empty>暂无记录</template></el-table
  >
</template>
<script setup lang="ts">
import { formatTime } from "@/utils/admin";
export interface Column {
  key: string;
  label: string;
  width?: number;
  time?: boolean;
  format?: (value: any, row: any) => string | number;
}
withDefaults(defineProps<{ rows: any[]; columns: Column[]; loading: boolean; actionWidth?: number }>(), { actionWidth: 180 });
</script>
