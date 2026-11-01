import { isRole, type Role } from "./permissions.js";

export interface Admin {
  id: number;
  username: string;
  role: Role;
  status: number;
}
export interface SessionState {
  token: string;
  expiresAt: number;
  admin: Admin | null;
  revision: number;
}
type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;
export const SESSION_KEY = "2dround.session";
export function isAdmin(value: unknown): value is Admin {
  const a = value as Admin | null;
  return (
    !!a &&
    Number.isSafeInteger(a.id) &&
    a.id > 0 &&
    typeof a.username === "string" &&
    a.username.length > 0 &&
    a.status === 1 &&
    isRole(a.role)
  );
}

// No administrator/role is restored from browser storage: /me must verify it.
export class AdminSession {
  private current: SessionState = { token: "", expiresAt: 0, admin: null, revision: 0 };
  private listeners = new Set<(state: SessionState) => void>();
  constructor(
    private storage?: Storage,
    private now = () => Date.now()
  ) {
    try {
      const cached = JSON.parse(storage?.getItem(SESSION_KEY) || "null");
      if (cached && this.validCredentials(cached.token, cached.expiresAt)) {
        this.current = { ...this.current, token: cached.token, expiresAt: cached.expiresAt };
      } else storage?.removeItem(SESSION_KEY);
    } catch {
      try {
        storage?.removeItem(SESSION_KEY);
      } catch {
        /* memory only */
      }
    }
  }
  get state(): Readonly<SessionState> {
    return this.current;
  }
  subscribe(listener: (state: SessionState) => void) {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }
  private validCredentials(token: unknown, expiresAt: unknown): boolean {
    return (
      typeof token === "string" &&
      token.length > 0 &&
      typeof expiresAt === "number" &&
      Number.isFinite(expiresAt) &&
      expiresAt * 1000 > this.now()
    );
  }
  private notify() {
    this.listeners.forEach(listener => listener(this.current));
  }
  start(token: string, expiresAt: number) {
    if (!this.validCredentials(token, expiresAt)) throw new Error("登录凭证无效或已过期，请重新登录");
    this.current = { token, expiresAt, admin: null, revision: this.current.revision + 1 };
    try {
      this.storage?.setItem(SESSION_KEY, JSON.stringify({ token, expiresAt }));
    } catch {
      /* memory only */
    }
    this.notify();
  }
  confirm(admin: unknown, revision: number): boolean {
    if (revision !== this.current.revision || !this.current.token) return false;
    if (!isAdmin(admin)) {
      this.clear(revision);
      throw new Error("管理员账号状态或角色无效，无法进入后台");
    }
    this.current = { ...this.current, admin };
    this.notify();
    return true;
  }
  clear(revision = this.current.revision): boolean {
    if (revision !== this.current.revision) return false;
    this.current = { token: "", expiresAt: 0, admin: null, revision: revision + 1 };
    try {
      this.storage?.removeItem(SESSION_KEY);
    } catch {
      /* memory only */
    }
    this.notify();
    return true;
  }
  expire(): boolean {
    if (this.current.token && this.current.expiresAt * 1000 <= this.now()) return this.clear();
    return false;
  }
}
