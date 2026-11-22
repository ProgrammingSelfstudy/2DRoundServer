package proto

// H5 shop commands use server-defined offers. The buy request echoes the
// catalog quote and carries a request ID to reuse when retrying that order.
const (
	H5ShopCatalog uint16 = 60720
	H5ShopBuy     uint16 = 60721
)

func registerH5Shop(s *Schema) {
	s.Send[H5ShopCatalog] = nil
	s.Recv[H5ShopCatalog] = []Field{
		{Name: "code", Type: "uint8"},
		{Name: "msg", Type: "string"},
		{Name: "sections", Type: "array", Fields: []Field{
			{Name: "name", Type: "string"},
			{Name: "currency_id", Type: "uint32"},
			{Name: "currency_name", Type: "string"},
			{Name: "balance", Type: "uint64"},
			{Name: "offers", Type: "array", Fields: []Field{
				{Name: "offer_id", Type: "int32"},
				{Name: "item_id", Type: "uint32"},
				{Name: "name", Type: "string"},
				{Name: "bundle", Type: "uint32"},
				{Name: "price", Type: "uint32"},
				{Name: "period", Type: "uint8"}, // 0 unlimited, 1 daily, 2 weekly, 3 monthly
				{Name: "remaining", Type: "uint32"},
			}},
		}},
	}
	s.Send[H5ShopBuy] = []Field{
		{Name: "request_id", Type: "string"},
		{Name: "offer_id", Type: "int32"},
		{Name: "item_id", Type: "uint32"},
		{Name: "currency_id", Type: "uint32"},
		{Name: "bundle", Type: "uint32"},
		{Name: "price", Type: "uint32"},
		{Name: "count", Type: "uint16"},
	}
	s.Recv[H5ShopBuy] = []Field{
		{Name: "request_id", Type: "string"},
		{Name: "code", Type: "uint8"},
		{Name: "msg", Type: "string"},
		{Name: "offer_id", Type: "int32"},
		{Name: "item_id", Type: "uint32"},
		{Name: "amount", Type: "uint32"},
		{Name: "currency_id", Type: "uint32"},
		{Name: "cost", Type: "uint64"},
	}
}
