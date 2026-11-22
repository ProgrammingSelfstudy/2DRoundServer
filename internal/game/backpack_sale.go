package game

const bagEquipment = 3

func allowsSaleStorage(storage int64, h5 bool) bool {
	return storage == bagBackpack || h5 && storage == bagEquipment
}

func saleableItem(id, storage int64) bool {
	info, known := itemTable[id]
	return known && len(info.Value) > 0 && saleStorageMatches(id, storage, true)
}

func saleStorageMatches(id, storage int64, h5 bool) bool {
	switch storage {
	case bagBackpack:
		if !h5 {
			info, known := itemTable[id]
			return known && goesToBackpack(info.Type)
		}
		return ItemListedInBackpack(id) && !isArtifact(id)
	case bagEquipment:
		_, ok := equipmentTable[id]
		return ok
	default:
		return false
	}
}

func saleInstanceMatches(id, bid, storage int64) bool {
	if storage == bagEquipment {
		return id == equipmentStackID(bid)
	}
	return id == bid
}
