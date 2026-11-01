import { onBeforeUnmount, ref, shallowRef } from "vue";
import { api } from "@/auth/runtime";
import { query } from "@/api/admin";
import { errorMessage } from "@/utils/admin";
// A later filter/navigation/session response can never replace the current rows.
export function useCollection<T>(path: string, limit = 25, paged = true) {
  const rows = shallowRef<T[]>([]),
    loading = ref(false),
    error = ref(""),
    page = ref(1),
    total = ref(0),
    extra = shallowRef<Record<string, unknown>>({});
  let seq = 0,
    active = true;
  async function load(filters: Record<string, string | number> = {}) {
    const current = ++seq;
    loading.value = true;
    error.value = "";
    try {
      const data = await api<{ items: T[]; total?: number; [key: string]: unknown }>(
        path + query({ ...filters, limit, ...(paged ? { offset: (page.value - 1) * limit } : {}) })
      );
      if (!active || seq !== current) return false;
      rows.value = data.items || [];
      total.value = data.total ?? rows.value.length;
      extra.value = data;
      return true;
    } catch (e) {
      if (active && seq === current) {
        error.value = errorMessage(e);
        rows.value = [];
        total.value = 0;
      }
      return false;
    } finally {
      if (active && seq === current) loading.value = false;
    }
  }
  onBeforeUnmount(() => {
    active = false;
    seq++;
  });
  return { rows, loading, error, page, total, extra, load };
}
