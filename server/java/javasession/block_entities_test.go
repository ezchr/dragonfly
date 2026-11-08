package javasession

import "testing"

func TestJavaBlockEntityName(t *testing.T) {
	for bedrock, want := range map[string]string{
		"minecraft:undyed_shulker_box":                 "minecraft:shulker_box",
		"minecraft:red_shulker_box":                    "minecraft:shulker_box",
		"minecraft:chest":                              "minecraft:chest",
		"minecraft:trapped_chest":                      "minecraft:trapped_chest",
		"minecraft:ender_chest":                        "minecraft:ender_chest",
		"minecraft:oak_hanging_sign":                   "minecraft:hanging_sign",
		"minecraft:standing_sign":                      "minecraft:sign",
		"minecraft:spruce_wall_sign":                   "minecraft:sign",
		"minecraft:player_head":                        "minecraft:skull",
		"minecraft:standing_banner":                    "minecraft:banner",
		"minecraft:bed":                                "minecraft:bed",
		"minecraft:copper_golem_statue":                "minecraft:copper_golem_statue",
		"minecraft:waxed_oxidized_copper_golem_statue": "minecraft:copper_golem_statue",
		"minecraft:stone":                              "",
		"minecraft:barrel":                             "", // drawn as a plain block
	} {
		if got := javaBlockEntityName(bedrock); got != want {
			t.Errorf("javaBlockEntityName(%q) = %q, want %q", bedrock, got, want)
		}
	}
}
