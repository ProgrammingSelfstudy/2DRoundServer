import { AdminSession } from "./session";
import { createApi } from "@/api/client";
let storage: Storage | undefined;
try {
  storage = window.sessionStorage;
} catch {
  /* private mode: in-memory session */
}
export const session = new AdminSession(storage);
export const api = createApi(session);
