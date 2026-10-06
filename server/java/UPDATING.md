# Updating the Java crossplay

How to move `server/java` to a new Java version, a new Bedrock version or a newer Dragonfly. Commands
run from the repository root (`/root/javaproto/dragonfly`) with `GOWORK=off`. Reference data lives
outside the repository, in `/root/javaproto/refs` (`$R` below).

The Bedrock -> Java tables (`javamap`) are updated by `internal/javamapupdate`. It needs only Mojang's
reports and Dragonfly's registries:

- **Carry over.** It carries every decision in `javamap/decisions.txt` over by name and properties.
- **Decide new ones.** It decides new Bedrock states, items and biomes by rules learned from those
  decisions, plus the small `internal/javamapupdate/handrules.go`.
- **Report.** It writes everything it decided, guessed or could not map to `javamap/REPORT.md`.

GeyserMC's mappings are optional: `-geyser DIR` only adds a cross-check section to the report.

```sh
R=/root/javaproto/refs
export GOWORK=off
```

## New Java version

Example: 26.3 -> 26.4. Below, `v778` is the new protocol package and `v777` the old newest.

1. **Mojang data.** Get the server jar and run the data generator:

   ```sh
   mkdir -p $R/mojang-26.4 && cd $R/mojang-26.4
   # server.jar: piston-meta.mojang.com/mc/game/version_manifest_v2.json -> 26.4.json -> downloads.server
   java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports --output generated
   ```

   Also fetch `client.jar` (same JSON, `downloads.client`) for step 5.

2. **Capture a vanilla join, then run javagen.**
   1. Set up a vanilla 26.4 server: copy `/root/javaproto/vanilla262` to `/root/javaproto/vanilla264`, put
      the new `server.jar` in, and pick a free `server-port`, e.g. 25604. Keep `online-mode=false`.
   2. Start it in tmux.
   3. Capture a join through the proxy, the way `tools/capture/vanilla262` was made:

   ```sh
   T=/root/javaproto/tools
   $T/pktcap/pktcap -listen 127.0.0.1:25664 -server 127.0.0.1:25604 \
       -packets $R/mojang-26.4/generated/reports/packets.json -out $T/capture/vanilla264 &
   VER=26.4 $T/client.sh 127.0.0.1:25664 40 /tmp/v264-client.log   # headless real client
   kill %1
   go run ./server/java/protocol/cmd/javagen -packets $R/mojang-26.4/generated/reports/packets.json \
       -registries $R/mojang-26.4/generated/reports/registries.json \
       -capture $T/capture/vanilla264/conn001 -out server/java/protocol/v778 -pkg v778
   ```

   `run/fx-vcap.sh` and `run/fx262-vcap.sh` show how to capture sounds, particles and effects too, by
   sending console commands while the client is connected.

3. **Keep the previous Java versions working with remapgen.** 26.4 becomes the newest and every older
   version is remapped from it:

   ```sh
   go run ./server/java/protocol/cmd/remapgen -old $R/mojang-26.3/generated/reports \
       -new $R/mojang-26.4/generated/reports -out server/java/protocol/v777/remap_gen.go -pkg v777
   go run ./server/java/protocol/cmd/remapgen -old $R/mojang-26.2/generated/reports \
       -new $R/mojang-26.4/generated/reports -out server/java/protocol/v776/remap_gen.go -pkg v776
   ```

   Then switch the code from the old newest package to the new one:
   1. Copy `v776/remap.go` into `v777` and add a `V777` entry in `protocol/version/version.go`, like `V776`.
   2. Point `Newest` at `v778`.
   3. Replace the remaining `v777` imports with `v778` (`grep -rn protocol/v777 server/java`).
   4. remapgen stops if handshake, status or login packets changed; remap those by hand.

4. **Run the update tool.**

   ```sh
   go run ./server/java/internal/javamapupdate -java 26.4 -reports $R/mojang-26.4/generated/reports \
       -biomes server/java/protocol/v778/registries.go -out server/java/javamap \
       -sounds server/java/javasession/fx_customsound_gen.go [-geyser $R/geyser-mappings-26.4]
   ```

   1. Put the same arguments in the `go:generate` line of `javamap/doc.go`.
   2. Review `javamap/REPORT.md` (see "Reviewing the report" below). Expect:
      - mostly `carried` (the ids shifted);
      - `carried-closest` where a Java block changed its properties;
      - `carried-substitute` where Mojang removed or renamed a block.

      Add a rename to `javaRenames` in `handrules.go` and rerun when a stand-in is wrong.
   3. Regenerate `javasession/fx_blocksound_gen.go` with `/root/javaproto/tools/fxgen`: compile and run
      `FxDump.java` against the new server jar, then `fxgen.py DUMP OUT`. It is generated from the jar.

5. **Find changed packet layouts.** Decompile both client jars and diff the packet classes:

   ```sh
   java -jar $R/tools/vineflower.jar $R/mojang-26.3/client.jar /tmp/dec263
   java -jar $R/tools/vineflower.jar $R/mojang-26.4/client.jar /tmp/dec264
   diff -r /tmp/dec263/net/minecraft/network /tmp/dec264/net/minecraft/network | less
   ```

   Also diff `generated/reports/packets.json` for added, removed and moved packets, and the
   `net/minecraft/network/syncher` and `world/item/component` classes (entity metadata, item
   components). Write per-version encoders where layouts changed (see `Version.Protocol`).

6. **Test.** See [Testing](#testing). Test both the new version and every older one (`VER=26.3`, `VER=26.2`).

## New Bedrock version or newer Dragonfly

1. **Merge.**

   ```sh
   git remote add upstream https://github.com/df-mc/dragonfly.git   # once
   git fetch upstream
   git checkout master && git merge upstream/master
   git checkout crossplay && git merge master
   ```

   Resolve conflicts in the hooks this branch adds to Dragonfly's packages (player session
   interface, `Server.LoadPlayer`/`AddPlayer`, the player list).
2. **gophertunnel patch.** If the merge bumps gophertunnel, re-apply the local patch to the server
   that runs this branch. The live server keeps a patched copy in
   `/root/dragonfly/third_party/gophertunnel` (one line, `PlatformBroadcastMode`, see its
   `PATCH.md`). Copy the new version there, re-apply the line and keep the `replace` in its go.mod.
3. **Run the update tool.** The Java version did not change, so `go generate` is enough:

   ```sh
   go generate ./server/java/javamap
   ```

   It carries every decision whose Bedrock state still exists and decides the new states. Decisions for
   states Dragonfly dropped are listed in the report and removed.
4. **Review `javamap/REPORT.md`**, then fix `handrules.go` where needed and rerun.
5. **Test.** See [Testing](#testing).

## Reviewing the report

`REPORT.md` starts with a summary, then a **For review** section:

- **New block decisions**, one line per Bedrock name: the Java block, how it was chosen, how each Java
  property was chosen, and **LOW** with the reasons when the decision is a guess.
  - `analog` means a block of the same family with the same Bedrock state gave the answer, for example
    a new wood type copying `dark_oak_*`. These are reliable.
  - Read every **LOW** line.
- **Misses**: blocks sent as stone, items sent as barrier.
- **Carried decisions whose Java state changed**, and any stand-ins.
- **NeighbourDependent changes**, and the Java blocks no Bedrock state maps to.

To fix a decision:

1. Add a hand rule: `handBlocks`, `handBlockAliases`, `handItems`, `handBiomes`, `javaRenames` or
   `javaTokenSubstitutes`.
2. Delete the decision from `decisions.txt` (the state's line, or a block's whole `B` group) so it is
   decided again.
3. Rerun.

A new property encoding that no other block uses (for example a bit field such as
`chiseled_bookshelf`'s `books_stored`) cannot be learned. It shows up as **LOW** with the Java default.
Run with `-geyser` to compare with GeyserMC, or write the decisions by hand into `decisions.txt` and
rerun.

To check how well the rules do on the current data, use leave-one-out. It hides 20% of the decisions,
recovers them, and writes nothing:

```sh
go run ./server/java/internal/javamapupdate -reports $R/mojang-26.3/generated/reports \
    -biomes server/java/protocol/v777/registries.go -prev server/java/javamap -loo 0.2 [-loo-hand]
```

## Testing

```sh
go vet ./server/java/... && go test -race ./server/java/... && go build ./...
```

`javamap`'s tests fail when a Dragonfly state is missing from the generated table.

Live tests, against `cmd/dfjtest` (Java on 127.0.0.1:25620, Bedrock on 127.0.0.1:19160):

```sh
go run ./server/java/cmd/dfjtest -noauth &                      # test server, superflat world
go run ./server/java/protocol/cmd/jbot -addr 127.0.0.1:25620 -secs 20      # scripted Java client
go run ./server/java/cmd/bedrockbot -addr 127.0.0.1:19160 -secs 20         # Bedrock client: sees the Java player?
/root/javaproto/tools/client.sh 127.0.0.1:25620 40 /tmp/client.log           # real headless Java client
VER=26.2 /root/javaproto/tools/client.sh 127.0.0.1:25620 40 /tmp/client262.log   # an older version
```

`client.sh` prints the client's connection and disconnect lines. The whole client log is in the
log file. `jbot` flags `-break`, `-attack` and `-cmd` exercise block breaking, combat and commands.

Do not test against the live server (`/root/dragonfly`).
