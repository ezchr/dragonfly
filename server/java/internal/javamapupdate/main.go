// Command javamapupdate updates the Bedrock -> Java id tables of package javamap (blocks, items, biomes) and the
// Bedrock -> Java sound names of javasession for a new Java version, a new Bedrock version or a newer Dragonfly.
// It needs only Mojang's reports of the target Java version, the previous tables and Dragonfly's registries;
// GeyserMC's mappings are an optional cross-check.
//
// Every previous decision is carried over by Bedrock state and Java name + properties (decisions.txt), so a Java
// version bump only renumbers. Bedrock states, items and biomes without a decision are decided by rules learned
// from the existing decisions (see learn.go) plus a small hand table (handrules.go). REPORT.md lists every new
// decision, low-confidence guess and miss. See server/java/UPDATING.md.
//
//	go run ./server/java/internal/javamapupdate -java 26.3 \
//		-reports /root/javaproto/refs/mojang-26.3/generated/reports \
//		-biomes server/java/protocol/v777/registries.go \
//		-out server/java/javamap -sounds server/java/javasession/fx_customsound_gen.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

var (
	javaVer     = flag.String("java", "", "target Java version, e.g. 26.3 (default: taken from -reports, .../mojang-<version>/...)")
	reportsDir  = flag.String("reports", "", "Mojang generated/reports directory of the target Java version (blocks.json, registries.json)")
	biomesFile  = flag.String("biomes", "", "javagen registries.go of the target version (protocol/vNNN/registries.go), for the Java biome names")
	outDir      = flag.String("out", ".", "javamap package directory to write")
	prevDir     = flag.String("prev", "", "javamap directory holding the previous tables (default: -out)")
	prevReports = flag.String("prev-reports", "", "bootstrap only: Mojang reports the previous _gen.go tables were made for, when -prev has no decisions.txt")
	prevJava    = flag.String("prev-java", "", "bootstrap only: Java version of the previous tables (default: taken from -prev-reports)")
	geyserDir   = flag.String("geyser", "", "optional GeyserMC mappings directory for the target Java version: cross-check only")
	soundsFile  = flag.String("sounds", "", "optional javasession/fx_customsound_gen.go to check and rewrite")
	loo         = flag.Float64("loo", 0, "leave-one-out evaluation: hide this fraction of the decisions, print the accuracy, write nothing")
	looSeed     = flag.Uint64("seed", 1, "random seed for -loo")
	looHand     = flag.Bool("loo-hand", false, "use the hand rules during -loo (default: rules learned from data only)")
	dryRun      = flag.Bool("n", false, "write nothing but REPORT.md (to -out)")
)

var versionInPath = regexp.MustCompile(`mojang-([0-9][0-9.a-z-]*)`)

func main() {
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("javamapupdate: ")
	if *reportsDir == "" || *biomesFile == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *javaVer == "" {
		m := versionInPath.FindStringSubmatch(*reportsDir)
		if m == nil {
			log.Fatal("pass -java: cannot tell the Java version from -reports")
		}
		*javaVer = m[1]
	}
	if *prevDir == "" {
		*prevDir = *outDir
	}

	jd := loadJava(filepath.Join(*reportsDir, "blocks.json"))
	reg := loadRegistries(filepath.Join(*reportsDir, "registries.json"))
	javaBiomes := loadBiomeNames(*biomesFile)
	df := dragonflyStates()
	dfItems := dragonflyItems()
	dfBio := dragonflyBiomes()

	var prev *prevTables
	if _, err := os.Stat(filepath.Join(*prevDir, decisionsFile)); err == nil {
		prev = readDecisions(filepath.Join(*prevDir, decisionsFile))
	} else {
		pj := *prevJava
		if pj == "" {
			if m := versionInPath.FindStringSubmatch(*prevReports); m != nil {
				pj = m[1]
			}
		}
		prev = bootstrap(*prevDir, *prevReports, pj, df)
	}

	if *loo > 0 {
		fmt.Print(runLOO(jd, prev, df, dfItems, dfBio, javaBiomes, reg, *loo, *looSeed, *looHand))
		return
	}

	br := updateBlocks(jd, prev, df, true)
	var blockPairs []*pair
	for _, d := range br.decisions {
		if d.origin == "miss" {
			continue // a block miss (stone) says nothing about the item
		}
		s := jd.states[d.java]
		blockPairs = append(blockPairs, newPair(d.df.b, s.block, s.props))
	}
	ir := updateItems(reg, prev, dfItems, blockPairs, true)
	bi := updateBiomes(javaBiomes, prev, dfBio, true)
	var sr *soundsResult
	if *soundsFile != "" {
		sr = updateSounds(*soundsFile, reg, *javaVer)
	}
	var gc *geyserCheck
	if *geyserDir != "" {
		gc = crossCheck(*geyserDir, jd, reg, br, ir, bi, dfItems)
		if sr != nil {
			sr.geyser = crossCheckSounds(*geyserDir, reg, sr)
		}
	}

	ri := runInfo{javaVer: *javaVer, prevJava: prev.java, prevSource: prev.source, geyserDir: *geyserDir}
	if !*dryRun {
		writeBlocks(*outDir, *javaVer, jd, br)
		writeItems(*outDir, *javaVer, reg, ir)
		writeBiomes(*outDir, *javaVer, bi)
		writeDecisions(filepath.Join(*outDir, decisionsFile), decisionsOf(*javaVer, jd, br, ir, bi))
		if sr != nil {
			writeSounds(sr)
		}
	}
	writeReport(*outDir, ri, jd, br, ir, bi, sr, gc)

	n := 0
	for _, d := range br.decisions {
		if d.status != statusCarried {
			n++
		}
	}
	log.Printf("Java %s: %d block states (%d new or changed), %d item entries (%d new, %d misses), %d biomes (%d new); see %s",
		*javaVer, len(br.decisions), n, len(ir.entries), len(ir.newOnes), len(ir.misses), len(bi.entries), len(bi.newOnes),
		filepath.Join(*outDir, "REPORT.md"))
}
