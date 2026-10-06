package javasession

// jsound is a Java sound event the sound mapping uses; soundIDs holds its registry id.
type jsound uint16

const (
	sndAttackStrong jsound = iota
	sndAttackNoDamage
	sndHurtDrown
	sndHurtOnFire
	sndBigFall
	sndSmallFall
	sndBurp
	sndItemPickup
	sndExplode
	sndThunder
	sndLightningImpact
	sndLevelUp
	sndExperienceOrb
	sndFireworkLaunch
	sndFireworkLargeBlast
	sndFireworkBlast
	sndFireworkTwinkle
	sndWoolPlace
	sndWoolStep
	sndWoolBreak
	sndSplashPotionBreak
	sndLavaExtinguish
	sndChestOpen
	sndChestClose
	sndEnderChestOpen
	sndEnderChestClose
	sndBarrelOpen
	sndBarrelClose
	sndShulkerBoxOpen
	sndShulkerBoxClose
	sndDeny
	sndWoodenDoorOpen
	sndWoodenDoorClose
	sndWoodenTrapdoorOpen
	sndWoodenTrapdoorClose
	sndFenceGateOpen
	sndFenceGateClose
	sndFlintAndSteel
	sndTNTPrimed
	sndItemFrameAdd
	sndItemFrameRemove
	sndItemFrameRotate
	sndFurnaceCrackle
	sndCampfireCrackle
	sndBlastFurnaceCrackle
	sndSmokerSmoke
	sndComposterEmpty
	sndComposterFill
	sndComposterFillSuccess
	sndComposterReady
	sndBrewingStandBrew
	sndLeverClick
	sndBookPut
	sndWaxOn
	sndWaxOff
	sndScrape
	sndSignWaxedFail
	sndDecoratedPotInsert
	sndDecoratedPotInsertFail
	sndItemBreak
	sndShieldBlock
	sndSnowballThrow
	sndHoeTill
	sndShovelFlatten
	sndAxeStrip
	sndEat
	sndEquipGeneric
	sndEquipLeather
	sndEquipCopper
	sndEquipGold
	sndEquipChain
	sndEquipIron
	sndEquipDiamond
	sndEquipNetherite
	sndEquipElytra
	sndBucketFill
	sndBucketFillLava
	sndBucketEmpty
	sndBucketEmptyLava
	sndArrowShoot
	sndCrossbowLoadingStart
	sndCrossbowLoadingMiddle
	sndCrossbowLoadingEnd
	sndCrossbowQuickCharge1
	sndCrossbowQuickCharge2
	sndCrossbowQuickCharge3
	sndCrossbowShoot
	sndArrowHit
	sndPlayerTeleport
	sndSpyglassUse
	sndSpyglassStop
	sndGoatHorn0
	sndGoatHorn1
	sndGoatHorn2
	sndGoatHorn3
	sndGoatHorn4
	sndGoatHorn5
	sndGoatHorn6
	sndGoatHorn7
	sndFireChargeUse
	sndTotemUse
	sndNoteHarp // 16 note block instruments follow, in sound.Instrument order
	sndNoteBaseDrum
	sndNoteSnare
	sndNoteHat
	sndNoteBass
	sndNoteFlute
	sndNoteBell
	sndNoteGuitar
	sndNoteChime
	sndNoteXylophone
	sndNoteIronXylophone
	sndNoteCowBell
	sndNoteDidgeridoo
	sndNoteBit
	sndNoteBanjo
	sndNotePling
	sndCount
)

// soundNames are the Java 26.3 sound event names (without "minecraft:").
var soundNames = [sndCount]string{
	sndAttackStrong:           "entity.player.attack.strong",
	sndAttackNoDamage:         "entity.player.attack.nodamage",
	sndHurtDrown:              "entity.player.hurt_drown",
	sndHurtOnFire:             "entity.player.hurt_on_fire",
	sndBigFall:                "entity.player.big_fall",
	sndSmallFall:              "entity.player.small_fall",
	sndBurp:                   "entity.player.burp",
	sndItemPickup:             "entity.item.pickup",
	sndExplode:                "entity.generic.explode",
	sndThunder:                "entity.lightning_bolt.thunder",
	sndLightningImpact:        "entity.lightning_bolt.impact",
	sndLevelUp:                "entity.player.levelup",
	sndExperienceOrb:          "entity.experience_orb.pickup",
	sndFireworkLaunch:         "entity.firework_rocket.launch",
	sndFireworkLargeBlast:     "entity.firework_rocket.large_blast",
	sndFireworkBlast:          "entity.firework_rocket.blast",
	sndFireworkTwinkle:        "entity.firework_rocket.twinkle",
	sndWoolPlace:              "block.wool.place",
	sndWoolStep:               "block.wool.step",
	sndWoolBreak:              "block.wool.break",
	sndSplashPotionBreak:      "entity.splash_potion.break",
	sndLavaExtinguish:         "block.lava.extinguish",
	sndChestOpen:              "block.chest.open",
	sndChestClose:             "block.chest.close",
	sndEnderChestOpen:         "block.ender_chest.open",
	sndEnderChestClose:        "block.ender_chest.close",
	sndBarrelOpen:             "block.barrel.open",
	sndBarrelClose:            "block.barrel.close",
	sndShulkerBoxOpen:         "block.shulker_box.open",
	sndShulkerBoxClose:        "block.shulker_box.close",
	sndDeny:                   "block.note_block.basedrum",
	sndWoodenDoorOpen:         "block.wooden_door.open",
	sndWoodenDoorClose:        "block.wooden_door.close",
	sndWoodenTrapdoorOpen:     "block.wooden_trapdoor.open",
	sndWoodenTrapdoorClose:    "block.wooden_trapdoor.close",
	sndFenceGateOpen:          "block.fence_gate.open",
	sndFenceGateClose:         "block.fence_gate.close",
	sndFlintAndSteel:          "item.flintandsteel.use",
	sndTNTPrimed:              "entity.tnt.primed",
	sndItemFrameAdd:           "entity.item_frame.add_item",
	sndItemFrameRemove:        "entity.item_frame.remove_item",
	sndItemFrameRotate:        "entity.item_frame.rotate_item",
	sndFurnaceCrackle:         "block.furnace.fire_crackle",
	sndCampfireCrackle:        "block.campfire.crackle",
	sndBlastFurnaceCrackle:    "block.blastfurnace.fire_crackle",
	sndSmokerSmoke:            "block.smoker.smoke",
	sndComposterEmpty:         "block.composter.empty",
	sndComposterFill:          "block.composter.fill",
	sndComposterFillSuccess:   "block.composter.fill_success",
	sndComposterReady:         "block.composter.ready",
	sndBrewingStandBrew:       "block.brewing_stand.brew",
	sndLeverClick:             "block.lever.click",
	sndBookPut:                "item.book.put",
	sndWaxOn:                  "item.honeycomb.wax_on",
	sndWaxOff:                 "item.axe.wax_off",
	sndScrape:                 "item.axe.scrape",
	sndSignWaxedFail:          "block.sign.waxed_interact_fail",
	sndDecoratedPotInsert:     "block.decorated_pot.insert",
	sndDecoratedPotInsertFail: "block.decorated_pot.insert_fail",
	sndItemBreak:              "entity.item.break",
	sndShieldBlock:            "item.shield.block",
	sndSnowballThrow:          "entity.snowball.throw",
	sndHoeTill:                "item.hoe.till",
	sndShovelFlatten:          "item.shovel.flatten",
	sndAxeStrip:               "item.axe.strip",
	sndEat:                    "entity.generic.eat",
	sndEquipGeneric:           "item.armor.equip_generic",
	sndEquipLeather:           "item.armor.equip_leather",
	sndEquipCopper:            "item.armor.equip_copper",
	sndEquipGold:              "item.armor.equip_gold",
	sndEquipChain:             "item.armor.equip_chain",
	sndEquipIron:              "item.armor.equip_iron",
	sndEquipDiamond:           "item.armor.equip_diamond",
	sndEquipNetherite:         "item.armor.equip_netherite",
	sndEquipElytra:            "item.armor.equip_elytra",
	sndBucketFill:             "item.bucket.fill",
	sndBucketFillLava:         "item.bucket.fill_lava",
	sndBucketEmpty:            "item.bucket.empty",
	sndBucketEmptyLava:        "item.bucket.empty_lava",
	sndArrowShoot:             "entity.arrow.shoot",
	sndCrossbowLoadingStart:   "item.crossbow.loading_start",
	sndCrossbowLoadingMiddle:  "item.crossbow.loading_middle",
	sndCrossbowLoadingEnd:     "item.crossbow.loading_end",
	sndCrossbowQuickCharge1:   "item.crossbow.quick_charge_1",
	sndCrossbowQuickCharge2:   "item.crossbow.quick_charge_2",
	sndCrossbowQuickCharge3:   "item.crossbow.quick_charge_3",
	sndCrossbowShoot:          "item.crossbow.shoot",
	sndArrowHit:               "entity.arrow.hit",
	sndPlayerTeleport:         "entity.player.teleport",
	sndSpyglassUse:            "item.spyglass.use",
	sndSpyglassStop:           "item.spyglass.stop_using",
	sndGoatHorn0:              "item.goat_horn.sound.0",
	sndGoatHorn1:              "item.goat_horn.sound.1",
	sndGoatHorn2:              "item.goat_horn.sound.2",
	sndGoatHorn3:              "item.goat_horn.sound.3",
	sndGoatHorn4:              "item.goat_horn.sound.4",
	sndGoatHorn5:              "item.goat_horn.sound.5",
	sndGoatHorn6:              "item.goat_horn.sound.6",
	sndGoatHorn7:              "item.goat_horn.sound.7",
	sndFireChargeUse:          "item.firecharge.use",
	sndTotemUse:               "item.totem.use",
	sndNoteHarp:               "block.note_block.harp",
	sndNoteBaseDrum:           "block.note_block.basedrum",
	sndNoteSnare:              "block.note_block.snare",
	sndNoteHat:                "block.note_block.hat",
	sndNoteBass:               "block.note_block.bass",
	sndNoteFlute:              "block.note_block.flute",
	sndNoteBell:               "block.note_block.bell",
	sndNoteGuitar:             "block.note_block.guitar",
	sndNoteChime:              "block.note_block.chime",
	sndNoteXylophone:          "block.note_block.xylophone",
	sndNoteIronXylophone:      "block.note_block.iron_xylophone",
	sndNoteCowBell:            "block.note_block.cow_bell",
	sndNoteDidgeridoo:         "block.note_block.didgeridoo",
	sndNoteBit:                "block.note_block.bit",
	sndNoteBanjo:              "block.note_block.banjo",
	sndNotePling:              "block.note_block.pling",
}
