import { api } from "@/auth/runtime";
export interface ServerRow {
  srv_id: string;
  name: string;
  platform: string;
  zone_id: number;
  host: string;
  port: number;
  ws_url: string;
  status: number;
  is_new: boolean;
  is_recommend: boolean;
  open_time: number;
  main_srv_id: string;
  group_id: number;
}
export interface RoleRow {
  rid: number;
  name: string;
  account: string;
  srv_id: string;
  lev: number;
  vip_lev?: number;
  status: number;
  created_at: number;
  last_login_at: number;
}
export interface RoleDetail {
  role: RoleRow;
  account: string;
  banned: boolean;
  online: boolean | null;
  online_error?: string;
  assets: { label: number; name: string; val: number }[];
  items: { base_id: number; name: string; num: number; listed: boolean }[];
}
export interface ItemChoice {
  base_id: number;
  name: string;
  currency: boolean;
  in_backpack: boolean;
}
export interface Meta {
  assets: { name: string; label: number }[];
  heroes: { bid: number; name: string; camp: number; init_star: number; max_star: number }[];
  skins: { id: number; name: string; bid: number }[];
  looks: { kind: string; id: number; name: string }[];
}
export interface PushReply {
  ok: boolean;
  refreshed?: boolean | null;
  push_error?: string;
}
export const getServers = () => api<{ items: ServerRow[] }>("/servers");
export const getMeta = () => api<Meta>("/meta");
export const getRole = (rid: number) => api<RoleDetail>(`/roles/${rid}`);
export function query(values: Record<string, string | number>) {
  const params = new URLSearchParams();
  Object.entries(values).forEach(([k, v]) => {
    if (v !== "") params.set(k, String(v));
  });
  return `?${params}`;
}
export function getRoles(q: string, srv_id: string, limit: number, offset: number) {
  return api<{ items: RoleRow[]; total: number }>(`/roles${query({ q, srv_id, limit, offset })}`);
}
export const searchItems = (q: string) => api<{ items: ItemChoice[] }>(`/items${query({ q, limit: 30 })}`);
