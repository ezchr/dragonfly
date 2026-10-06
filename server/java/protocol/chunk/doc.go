// Package chunk encodes and decodes the Java Edition 26.3 (protocol 777) level_chunk_with_light
// packet body, byte for byte as the vanilla server writes it.
//
// The format below was read from the 26.3 server jar (ClientboundLevelChunkWithLightPacket,
// ClientboundLevelChunkPacketData, ClientboundLightUpdatePacketData, LevelChunkSection,
// PalettedContainer, Strategy, Configuration, the four palettes, SimpleBitStorage, Heightmap,
// ByteBufCodecs, FriendlyByteBuf) and checked against bodies captured from a vanilla 26.3 server,
// which this package re-encodes exactly (testdata/).
//
//	Int      chunk x
//	Int      chunk z
//	VarInt   heightmap count, then per heightmap:
//	           VarInt type id (Heightmap.Types: 1 WORLD_SURFACE, 4 MOTION_BLOCKING,
//	                  5 MOTION_BLOCKING_NO_LEAVES are the ones sent)
//	           VarInt long count + longs (SimpleBitStorage, ceil(log2(height+1)) bits, 256 entries,
//	                  index x + z*16, value = first free y - min y; 37 longs for a 384 high world)
//	VarInt   byte length of the section data (at most 2 MiB), then every section bottom to top:
//	           Short  non-air block count
//	           Short  fluid count (blocks whose FluidState is not empty)
//	           block states container, biomes container
//	VarInt   block entity count, then per entry: Byte packed xz, Short y, VarInt type,
//	         NBT (nameless network NBT; a single 0x00 TAG_End when the update tag is empty)
//	light:   BitSet sky mask, BitSet block mask, BitSet empty sky mask, BitSet empty block mask,
//	         VarInt count + (VarInt 2048 + 2048 bytes) per set bit of the sky mask, ascending,
//	         the same for the block mask
//
// A paletted container is a Byte bits-per-entry, the palette, and then the storage longs with no
// length prefix (the reader derives the count from the bits): 64/bits entries per long, first
// entry in the low bits, entries never split across longs. The bits byte is the storage's bit
// width; the reader maps it to a configuration the way Strategy.getConfigurationForBitCount does:
//
//	blocks (4096, index y<<8|z<<4|x):   0 single value; 1-4 linear, stored as 4 bits;
//	                                    5-8 hashmap; anything else global
//	biomes (64, index y<<4|z<<2|x):     0 single value; 1-3 linear; anything else global
//
// Single value: VarInt id and no longs. Linear and hashmap: VarInt size + VarInt ids. Global: no
// palette, ids stored directly with ceil(log2(registry size)) bits: 16 for the 35723 block states
// of 26.3, 7 for the 67 vanilla biomes.
//
// Which configuration vanilla sends depends on the container's history, because in-memory palettes
// only grow. For any chunk that has been through disk (PalettedContainer.pack/unpack) the palette
// holds the distinct values in order of first appearance by index and the bits come from its size
// (getConfigurationForPaletteSize); the captures match that, and so does the Encoder by default.
// A Layout on a section forces the wire layout instead; Decode records one so packets from any
// sender round-trip exactly.
//
// Light: the masks have len(sections)+2 bits, bit 0 being the section below the world. A section
// is in the mask when its DataLayer is allocated (even if all zero), in the empty mask when the
// DataLayer exists but is unallocated with default 0, and in neither when there is no DataLayer.
//
// Differences from the wiki (Java Edition protocol, Chunk Data and Update Light / Chunk format):
//
//   - The four light masks are ByteBufCodecs.BIT_SET: VarInt byte count + BitSet.toByteArray()
//     (little-endian bytes, trailing zero bytes trimmed), not the wiki's VarInt count of longs
//     (BitSet.toLongArray). FriendlyByteBuf.writeBitSet still uses longs, but this packet does not
//     use it. The vanilla superflat chunk sends 01 07 / 00 / 00 / 01 07.
//   - Direct (global) block palettes use 16 bits per entry in 26.3 (35723 states), not 15.
//   - The heightmap list order is not fixed: vanilla collects the heightmaps into a HashMap keyed by
//     the enum, so its iteration order is whatever that map gives (1, 4, 5 in the captures).
//   - The empty masks are set only for DataLayers that exist but were never allocated; an
//     allocated all-zero layer is sent as a 2048-byte array, not flagged as empty.
//   - Block entity NBT is optional: an absent tag is the single byte 0x00.
//   - Points the wiki copy here gets right but older references do not: the fluid count Short after
//     the block count, data arrays without a length prefix, heightmaps as (VarInt type, long array)
//     pairs instead of an NBT compound, and VarInt-length-prefixed 2048-byte light arrays.
package chunk
