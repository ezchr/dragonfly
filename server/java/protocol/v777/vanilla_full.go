package v777

import (
	"embed"
	"sort"
	"sync"
)

// vanilla_full holds the registry_data packets vanilla sends a client that doesn't have the core
// pack (every entry with its data), captured by dfmain/zz_regcap, in the order of
// VanillaConfiguration's registry_data packets.
//
//go:embed vanilla_full/*.bin
var vanillaFullFS embed.FS

var (
	fullOnce sync.Once
	full     [][]byte
)

// FullRegistryData is the bodies of the registry_data packets with full data. Don't modify them.
func FullRegistryData() [][]byte {
	fullOnce.Do(func() {
		e, _ := vanillaFullFS.ReadDir("vanilla_full")
		sort.Slice(e, func(i, j int) bool { return e[i].Name() < e[j].Name() })
		for _, f := range e {
			b, err := vanillaFullFS.ReadFile("vanilla_full/" + f.Name())
			if err != nil {
				panic("v777: embedded " + f.Name() + ": " + err.Error())
			}
			full = append(full, b)
		}
	})
	return full
}
