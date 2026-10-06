// Package javamap maps Dragonfly (Bedrock) blocks, items, biomes and entities to Java Edition ids.
//
// The *_gen.go tables and REPORT.md are generated from the GeyserMC Java 26.3 <-> Bedrock 1.26.50 mappings and the
// Mojang 26.3 reports by internal/javamapgen; regenerate with go generate ./javamap from the module root.
package javamap

//go:generate go run ../internal/javamapgen -refs ../../refs -out .
