package game

import "2droundserver/internal/store"

// The 快乐币 panel of the client (vip/klb_panel.lua) sells products 30001 to 30020 of the charge table: 快乐币 (asset 41), gift boxes,
// 海楼石 and so on. They are not the diamond tiers of the top-up window: the panel shows `gold` as the price in yuan and sends it as
// `Money` to the page PAY_URI names (the server list's url_config_demo.lua), while `val` is the amount of 快乐币 times 100 and must not
// be read as a price. They give no diamonds; what they give is the table's `items`, a currency into the asset ledger and anything else
// into the item ledger. They add no VIP experience (`vip_exp` is 0 in the table).
const (
	klbFirstID = 30001
	klbLastID  = 30020
)

func isKlbProduct(p ChargeProduct) bool {
	return p.ID >= klbFirstID && p.ID <= klbLastID && p.Type == 0
}

// KlbProductByPrice finds the 快乐币 panel product that costs yuan yuan (each price is used once).
func KlbProductByPrice(yuan int64) (ChargeProduct, bool) {
	for id := int64(klbFirstID); id <= klbLastID; id++ {
		if p, ok := chargeTable[id]; ok && isKlbProduct(p) && p.Gold == yuan {
			return p, true
		}
	}
	return ChargeProduct{}, false
}

func init() {
	for id := int64(klbFirstID); id <= klbLastID; id++ {
		p, ok := chargeTable[id]
		if !ok || !isKlbProduct(p) {
			continue
		}
		RegisterChargeHook(id, func(_ *store.Store, _ *store.Role, p ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			assets, items := map[int64]int64{}, map[int64]int64{}
			for _, it := range p.Items {
				if _, currency := assetNameByLabel[it[0]]; currency {
					assets[it[0]] += it[1]
				} else {
					items[it[0]] += it[1]
				}
			}
			return assets, items, nil
		})
	}
}
