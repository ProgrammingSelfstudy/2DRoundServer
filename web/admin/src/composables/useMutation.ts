import { ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { ApiError } from "@/api/client";
import { useAuthStore } from "@/stores/modules/auth";
import type { Capability } from "@/auth/permissions";
import { errorMessage } from "@/utils/admin";
export async function confirmAction(message: string, title = "确认操作") {
  try {
    await ElMessageBox.confirm(message, title, {
      type: "warning",
      confirmButtonText: "确认",
      cancelButtonText: "取消",
      closeOnClickModal: false
    });
    return true;
  } catch {
    return false;
  }
}
export function useMutation(need: Capability = "write") {
  const busy = ref(false);
  const auth = useAuthStore();
  async function run(action: () => Promise<void>) {
    if (busy.value) return;
    busy.value = true;
    try {
      await auth.verifySession();
      if (!auth.hasPermission(need)) throw new ApiError(403, "your role may not do this");
      await action();
    } catch (error) {
      ElMessage.error(errorMessage(error));
    } finally {
      busy.value = false;
    }
  }
  return { busy, run };
}
