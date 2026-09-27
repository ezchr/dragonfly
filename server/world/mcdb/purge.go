package mcdb

import (
	"bytes"
	"encoding/binary"
	"slices"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// RemoveEntities deletes every saved entity that keep returns false for, in
// every chunk of every dimension, whether or not the chunk is ever loaded.
// keep is passed the entity's identifier, such as "minecraft:item". Terrain
// and block entities are left alone. Nothing else may have the database open
// while it runs. It returns how many entities it removed per identifier.
func (db *DB) RemoveEntities(keep func(id string) bool) (map[string]int, error) {
	removed := map[string]int{}
	batch := new(leveldb.Batch)
	identifier := func(data map[string]any) string {
		if id, ok := data["identifier"].(string); ok {
			return id
		}
		return "unknown"
	}

	// Current actor storage: a "digp" list of IDs per chunk, each entity
	// under its own "actorprefix" key.
	iter := db.ldb.NewIterator(util.BytesPrefix([]byte(keyEntityIdentifiers)), nil)
	for iter.Next() {
		key, ids := slices.Clone(iter.Key()), slices.Clone(iter.Value())
		var kept []byte
		for i := 0; i+8 <= len(ids); i += 8 {
			uid := int64(binary.LittleEndian.Uint64(ids[i:]))
			raw, err := db.ldb.Get(entityIndex(uid), nil)
			if err != nil {
				continue // listed but missing: drop it from the list
			}
			data := map[string]any{}
			_ = nbt.UnmarshalEncoding(raw, &data, nbt.LittleEndian)
			if id := identifier(data); !keep(id) {
				batch.Delete(entityIndex(uid))
				removed[id]++
				continue
			}
			kept = append(kept, ids[i:i+8]...)
		}
		switch {
		case len(kept) == 0:
			batch.Delete(key)
		case len(kept) != len(ids):
			batch.Put(key, kept)
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		return removed, err
	}

	// Older saves: NBT compounds appended under the chunk key plus
	// keyEntitiesOld. Chunk keys are 8 bytes, or 12 outside the overworld.
	iter = db.ldb.NewIterator(nil, nil)
	for iter.Next() {
		key := iter.Key()
		if (len(key) != 9 && len(key) != 13) || key[len(key)-1] != keyEntitiesOld {
			continue
		}
		key, value := slices.Clone(key), slices.Clone(iter.Value())
		var kept bytes.Buffer
		buf := bytes.NewBuffer(value)
		dec, enc := nbt.NewDecoderWithEncoding(buf, nbt.LittleEndian), nbt.NewEncoderWithEncoding(&kept, nbt.LittleEndian)
		for buf.Len() != 0 {
			data := map[string]any{}
			if err := dec.Decode(&data); err != nil {
				break // unreadable rest: drop it
			}
			if id := identifier(data); !keep(id) {
				removed[id]++
				continue
			}
			_ = enc.Encode(data)
		}
		if kept.Len() == 0 {
			batch.Delete(key)
		} else {
			batch.Put(key, kept.Bytes())
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		return removed, err
	}
	return removed, db.ldb.Write(batch, nil)
}
