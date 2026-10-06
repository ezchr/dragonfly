// Package javamap maps Dragonfly (Bedrock) blocks, items, biomes and entities to Java Edition ids.
//
// The *_gen.go tables are compiled by internal/javamapupdate from decisions.txt (every Bedrock -> Java decision by
// name and properties) and the Mojang 26.3 reports; it also writes REPORT.md. The decisions were first taken from
// GeyserMC's mappings; updating needs only Mojang's reports and Dragonfly's registries (see server/java/UPDATING.md).
// Regenerate with GOWORK=off go generate ./server/java/javamap from the repository root.
package javamap

//go:generate go run ../internal/javamapupdate -java 26.3 -reports ../../../../refs/mojang-26.3/generated/reports -biomes ../protocol/v777/registries.go -out . -sounds ../javasession/fx_customsound_gen.go
