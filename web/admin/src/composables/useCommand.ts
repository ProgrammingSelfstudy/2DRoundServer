import { ref } from "vue";
import { ElMessage } from "element-plus";
import { ApiError, StaleSessionError } from "@/api/client";
import { useAuthStore } from "@/stores/modules/auth";
import { SubmissionGate, ambiguousStatus } from "@/domain/operations";
import type { Capability } from "@/auth/permissions";
import { confirmAction } from "./useMutation";
import { errorMessage } from "@/utils/admin";
export function useCommand(need: Capability, nonIdempotent = false, knownUnchanged?: (error: ApiError) => boolean) {
  const gate = new SubmissionGate();
  const busy = ref(false);
  const uncertain = ref(false);
  const error = ref("");
  const auth = useAuthStore();
  async function execute<T>(action: () => Promise<T>, confirmation?: string): Promise<T | undefined> {
    if (!gate.begin()) return;
    busy.value = true;
    error.value = "";
    let attempted = false;
    try {
      await auth.verifySession();
      if (!auth.hasPermission(need)) throw new ApiError(403, "your role may not do this");
      if (confirmation && !(await confirmAction(confirmation))) return;
      attempted = true;
      return await action();
    } catch (e) {
      error.value = errorMessage(e);
      uncertain.value = nonIdempotent && attempted && e instanceof ApiError && ambiguousStatus(e.status) && !knownUnchanged?.(e);
      if (!(e instanceof StaleSessionError)) ElMessage.error(error.value);
    } finally {
      gate.finish(uncertain.value);
      busy.value = false;
    }
  }
  function reviewed() {
    gate.reviewed();
    uncertain.value = false;
    error.value = "";
  }
  return { busy, uncertain, error, execute, reviewed };
}
