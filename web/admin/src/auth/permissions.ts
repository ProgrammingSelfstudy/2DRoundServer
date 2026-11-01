export type Role = "viewer" | "operator" | "superadmin";
export type Capability = "read" | "write" | "manage";
const roles = { viewer: 1, operator: 2, superadmin: 3 } as const;
const capabilities = { read: 1, write: 2, manage: 3 } as const;
export const roleLabels: Record<Role, string> = {
  viewer: "只读管理员",
  operator: "运营管理员",
  superadmin: "超级管理员"
};
export function isRole(value: unknown): value is Role {
  return typeof value === "string" && Object.hasOwn(roles, value);
}
// Unknown roles and capabilities fail closed, including inherited object keys.
export function can(role: unknown, capability: unknown): boolean {
  return (
    isRole(role) &&
    typeof capability === "string" &&
    Object.hasOwn(capabilities, capability) &&
    roles[role] >= capabilities[capability as Capability]
  );
}
