package world

// LoadedStats reports how many chunk columns and entities the world holds in memory, for the
// server's memory log. Only valid inside the transaction.
func (tx *Tx) LoadedStats() (chunks, entities int) {
	return len(tx.w.chunks), len(tx.w.entities)
}
