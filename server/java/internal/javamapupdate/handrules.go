package main

// Hand rules: the only hand-written mapping data. Everything else is carried over from decisions.txt or learned
// from it. A hand rule is used only for a Bedrock state, item or biome that has no carried decision; it wins over
// learned name rewrites and same-name matches (it exists because those were wrong), but not over evidence from other
// states of the same Bedrock name.
//
// Keep this small: add an entry when REPORT.md shows a wrong automatic decision or a miss that no rule can learn.

// handBlocks maps Bedrock(-only) blocks with no Java counterpart of similar layout to a Java state (properties not
// given take the block's default; a Bedrock state's properties are ignored).
var handBlocks = map[string]string{
	"minecraft:border_block":                     "minecraft:barrier",
	"minecraft:chalkboard":                       "minecraft:oak_wall_sign",
	"minecraft:allow":                            "minecraft:bedrock",
	"minecraft:deny":                             "minecraft:bedrock",
	"minecraft:netherreactor":                    "minecraft:iron_block",
	"minecraft:glowingobsidian":                  "minecraft:crying_obsidian",
	"minecraft:frame":                            "minecraft:air", // Java item frames are entities
	"minecraft:glow_frame":                       "minecraft:air",
	"minecraft:client_request_placeholder_block": "minecraft:air",
	"minecraft:unknown":                          "minecraft:air",
	"minecraft:moving_block":                     "minecraft:air",
	"minecraft:portal_placeholder":               "minecraft:air",
	"minecraft:camera":                           "minecraft:observer",
	"minecraft:invisible_bedrock":                "minecraft:barrier",
	"minecraft:chemical_heat":                    "minecraft:furnace",
	"minecraft:reserved6":                        "minecraft:stone",
	"minecraft:info_update":                      "minecraft:stone",
	"minecraft:info_update2":                     "minecraft:stone",
	"minecraft:underwater_torch":                 "minecraft:torch",
	"minecraft:colored_torch_red":                "minecraft:redstone_torch",
	"minecraft:colored_torch_green":              "minecraft:torch",
	"minecraft:colored_torch_blue":               "minecraft:soul_torch",
	"minecraft:colored_torch_purple":             "minecraft:soul_torch",
	"minecraft:compound_creator":                 "minecraft:crafting_table",
	"minecraft:material_reducer":                 "minecraft:crafting_table",
	"minecraft:element_constructor":              "minecraft:crafting_table",
	"minecraft:lab_table":                        "minecraft:crafting_table",
}

// handBlockAliases predicts a Bedrock block from the evidence of another Bedrock name with the same state layout.
var handBlockAliases = map[string]string{
	"minecraft:water":                     "minecraft:flowing_water",
	"minecraft:flowing_water":             "minecraft:water",
	"minecraft:lava":                      "minecraft:flowing_lava",
	"minecraft:flowing_lava":              "minecraft:lava",
	"minecraft:deprecated_anvil":          "minecraft:anvil",
	"minecraft:deprecated_purpur_block_1": "minecraft:purpur_block",
	"minecraft:deprecated_purpur_block_2": "minecraft:purpur_block",
	"minecraft:underwater_tnt":            "minecraft:tnt",
}

// javaRenames maps Java block and item names a Java update removed to their new name. A carried decision whose
// Java block or item no longer exists is moved to the new name (its state via the closest properties); without an
// entry it is decided again by the rules. Example: 1.20.3 renamed grass to short_grass.
var javaRenames = map[string]string{
	"minecraft:grass": "minecraft:short_grass",
}

// handItems maps Bedrock items to Java items.
var handItems = map[string]string{
	"minecraft:glow_frame":           "minecraft:glow_item_frame",
	"minecraft:frame":                "minecraft:item_frame",
	"minecraft:fireworks":            "minecraft:firework_rocket",
	"minecraft:netherreactor":        "minecraft:iron_block",
	"minecraft:glowingobsidian":      "minecraft:crying_obsidian",
	"minecraft:border_block":         "minecraft:barrier",
	"minecraft:camera":               "minecraft:spyglass",
	"minecraft:chalkboard":           "minecraft:oak_sign",
	"minecraft:allow":                "minecraft:bedrock",
	"minecraft:deny":                 "minecraft:bedrock",
	"minecraft:underwater_torch":     "minecraft:torch",
	"minecraft:colored_torch_red":    "minecraft:redstone_torch",
	"minecraft:colored_torch_green":  "minecraft:torch",
	"minecraft:colored_torch_blue":   "minecraft:soul_torch",
	"minecraft:colored_torch_purple": "minecraft:soul_torch",
	"minecraft:compound_creator":     "minecraft:crafting_table",
	"minecraft:material_reducer":     "minecraft:crafting_table",
	"minecraft:element_constructor":  "minecraft:crafting_table",
	"minecraft:lab_table":            "minecraft:crafting_table",
	"minecraft:invisible_bedrock":    "minecraft:barrier",
}

// handItemPrefixes maps Bedrock item name prefixes to a Java item (light_block_0 ... light_block_15).
var handItemPrefixes = map[string]string{
	"minecraft:light_block_": "minecraft:light",
}

// handBiomes maps Bedrock biomes Java removed in 1.18 to the biome Java converts them to (Bedrock biome names).
var handBiomes = map[string]string{
	"legacy_frozen_ocean":              "minecraft:frozen_ocean",
	"ice_mountains":                    "minecraft:snowy_plains",
	"mushroom_island_shore":            "minecraft:mushroom_fields",
	"desert_hills":                     "minecraft:desert",
	"forest_hills":                     "minecraft:forest",
	"taiga_hills":                      "minecraft:taiga",
	"extreme_hills_edge":               "minecraft:windswept_hills",
	"jungle_hills":                     "minecraft:jungle",
	"birch_forest_hills":               "minecraft:birch_forest",
	"cold_taiga_hills":                 "minecraft:snowy_taiga",
	"mega_taiga_hills":                 "minecraft:old_growth_pine_taiga",
	"mesa_plateau":                     "minecraft:badlands",
	"deep_warm_ocean":                  "minecraft:warm_ocean",
	"bamboo_jungle_hills":              "minecraft:bamboo_jungle",
	"desert_mutated":                   "minecraft:desert",
	"taiga_mutated":                    "minecraft:taiga",
	"swampland_mutated":                "minecraft:swamp",
	"jungle_mutated":                   "minecraft:jungle",
	"jungle_edge_mutated":              "minecraft:sparse_jungle",
	"birch_forest_hills_mutated":       "minecraft:old_growth_birch_forest",
	"roofed_forest_mutated":            "minecraft:dark_forest",
	"cold_taiga_mutated":               "minecraft:snowy_taiga",
	"redwood_taiga_hills_mutated":      "minecraft:old_growth_spruce_taiga",
	"extreme_hills_plus_trees_mutated": "minecraft:windswept_forest",
	"savanna_plateau_mutated":          "minecraft:windswept_savanna",
	"mesa_plateau_stone_mutated":       "minecraft:wooded_badlands",
	"mesa_plateau_mutated":             "minecraft:badlands",
}

// soundRenames maps Java sound events a Java update removed to their new name.
var soundRenames = map[string]string{}

// propSource says where a Java property the Bedrock state does not carry comes from, for NeighbourDependent
// entries derived for Java blocks the previous tables never produced.
var propSource = map[string]string{
	"north": "SourceNeighbours", "east": "SourceNeighbours", "south": "SourceNeighbours", "west": "SourceNeighbours",
	"up": "SourceNeighbours", "down": "SourceNeighbours", "shape": "SourceNeighbours", "snowy": "SourceNeighbours",
	"type": "SourceNeighbours", "instrument": "SourceNeighbours", "signal_fire": "SourceNeighbours",
	"locked": "SourceNeighbours", "attached": "SourceNeighbours", "in_wall": "SourceNeighbours", "bottom": "SourceNeighbours",
	"powered": "SourceRedstone", "power": "SourceRedstone", "triggered": "SourceRedstone", "enabled": "SourceRedstone",
	"note": "SourceBlockEntity", "has_book": "SourceBlockEntity", "rotation": "SourceBlockEntity",
	"copper_golem_pose": "SourceBlockEntity", "facing": "SourceBlockEntity", "has_record": "SourceBlockEntity",
	"cracked": "SourceBlockEntity", "extended": "SourceBlockEntity", "mode": "SourceBlockEntity",
	"distance": "SourceNone", "stage": "SourceNone", "short": "SourceNone",
}

// javaTokenSubstitutes replaces name words of Java blocks and items the target version lacks (for tables made for
// an older Java version: poplar wood is 26.3-only). Without an entry, a block with the same properties and the same
// last word stands in.
var javaTokenSubstitutes = map[string]string{
	"poplar": "birch",
}
