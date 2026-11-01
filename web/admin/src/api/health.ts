import { api } from "@/auth/runtime";
export async function checkHealth(): Promise<void> {
  const data = await api<{ ok: boolean }>("/healthz", { public: true });
  if (data?.ok !== true) throw new Error("健康接口响应不符合预期");
}
