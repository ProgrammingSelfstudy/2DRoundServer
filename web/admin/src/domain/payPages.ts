/** What kind of product a payable product is, for the product list (ids are those of the client's charge table). */
export function productKind(id: number): string {
  if (id >= 30001 && id <= 30020) return "快乐币面板";
  if (id < 100) return "钻石充值档位";
  return "礼包与活动商品";
}
export const orderStatuses: Record<number, string> = { 0: "等待付款", 1: "已到账", 2: "已取消" };
export const yuanText = (cents: number) => (cents / 100).toLocaleString("zh-CN", { style: "currency", currency: "CNY" });
/** Only image data URLs are shown, the rule the payment page's QR codes and screenshots follow. */
export const isSafeImage = (value: string) => /^data:image\/(png|jpeg|webp);base64,/.test(value);
