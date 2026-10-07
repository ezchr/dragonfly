package world

import "sync"

// Item aliases: what a stored item that is no longer registered (an item a server removed) loads
// as, instead of disappearing from inventories and containers.

var (
	itemAliasMu sync.RWMutex
	itemAliases = map[string]string{}
)

// RegisterItemAlias makes stored items called name load as the item called to (meta 0) when name
// itself is not registered. Call it before players and worlds load.
func RegisterItemAlias(name, to string) {
	itemAliasMu.Lock()
	defer itemAliasMu.Unlock()
	itemAliases[name] = to
}

func itemAlias(name string) (string, bool) {
	itemAliasMu.RLock()
	defer itemAliasMu.RUnlock()
	to, ok := itemAliases[name]
	return to, ok
}
