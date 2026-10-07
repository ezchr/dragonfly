package chunk

import "sync"

// Block aliases: what a stored block that is no longer registered (a block a server removed, such
// as an add-on block it dropped) loads as. Without one, a chunk holding such a block fails to load.
// The alias is written back when the chunk is saved, so the world converts as chunks are used.

type blockAlias struct {
	name  string
	props map[string]any
}

var (
	aliasMu      sync.RWMutex
	blockAliases = map[string]blockAlias{}
)

// RegisterBlockAlias makes every stored state of the block called name load as the state toName
// with toProps (nil for none), when name itself is not registered. Call it before worlds load.
func RegisterBlockAlias(name, toName string, toProps map[string]any) {
	if toProps == nil {
		toProps = map[string]any{}
	}
	aliasMu.Lock()
	defer aliasMu.Unlock()
	blockAliases[name] = blockAlias{name: toName, props: toProps}
}

func aliasFor(name string) (blockAlias, bool) {
	aliasMu.RLock()
	defer aliasMu.RUnlock()
	a, ok := blockAliases[name]
	return a, ok
}
