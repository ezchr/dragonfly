package player

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"golang.org/x/text/language"
)

// Session is what a Player needs from the connection that controls it. The Bedrock session
// (*session.Session) is one; a Java Edition session is another. Generated from the methods of
// *session.Session that the player and server packages call.
type Session interface {
	world.Viewer

	AddDebugShape(shape debug.Shape)
	Addr() net.Addr
	ChunkRadius() int32
	ClearInputLocks()
	ClientData() login.ClientData
	Close(tx *world.Tx, c session.Controllable)
	CloseConnection()
	CloseDialogue()
	CloseForm()
	Disconnect(message string)
	EnableCoordinates(enable bool)
	EnableInstantRespawn(enable bool)
	HandleInventories(tx *world.Tx, c session.Controllable, inv *inventory.Inventory, offHand *inventory.Inventory, enderChest *inventory.Inventory, ui *inventory.Inventory, armour *inventory.Armour, heldSlot *uint32)
	HideHudElement(e hud.Element)
	HudElementHidden(e hud.Element) bool
	InputLocked(l input.Lock) bool
	Latency() time.Duration
	LockInput(l input.Lock)
	OpenBlockContainer(pos cube.Pos, tx *world.Tx)
	OpenSign(pos cube.Pos, frontSide bool)
	OpenTrade(tx *world.Tx, trader world.Entity, name string, offers []session.TradeOffer, onTrade func(index int, times int) bool)
	PlaySound(t world.Sound, pos mgl64.Vec3)
	RemoveAllDebugShapes()
	RemoveBossBar()
	RemoveDebugShape(shape debug.Shape)
	RemoveScoreboard()
	RemoveViewLayer(entity world.Entity)
	SendAbilities(c session.Controllable)
	SendActionBarMessage(text string)
	SendBossBar(text string, colour uint8, healthPercentage float64)
	SendChargeItemComplete()
	SendCommandOutput(output *cmd.Output, l language.Tag)
	SendDebugShapes(dim world.Dimension)
	SendDialogue(d dialogue.Dialogue, e world.Entity)
	SendEffect(e effect.Effect)
	SendEffectRemoval(e effect.Type)
	SendExperience(level int, progress float64)
	SendFood(food int, saturation float64, exhaustion float64)
	SendForm(f form.Form)
	SendGameMode(c session.Controllable)
	SendHealth(health float64, max float64, absorption float64)
	SendHeldSlot(slot int, c session.Controllable, force bool)
	SendHudUpdates()
	SendInputLocks()
	SendJukeboxPopup(message string)
	SendMessage(message string)
	SendPlayerSpawn(pos mgl64.Vec3)
	SendPopup(message string)
	SendRespawn(pos mgl64.Vec3, c session.Controllable)
	SendScoreboard(sb *scoreboard.Scoreboard)
	SendSpeed(speed float64)
	SendSubtitle(text string)
	SendTip(message string)
	SendTitle(text string)
	SendToast(title string, message string)
	SendTranslation(t chat.Translation, l language.Tag, a []any)
	SetHandle(handle *world.EntityHandle, skin skin.Skin)
	SetTitleDurations(fadeInDuration time.Duration, remainDuration time.Duration, fadeOutDuration time.Duration)
	ShowHudElement(e hud.Element)
	Spawn(c session.Controllable, tx *world.Tx)
	StartShowingEntity(e world.Entity)
	StopShowingEntity(e world.Entity)
	Transfer(ip net.IP, port int)
	UnlockInput(l input.Lock)
	UpdateTradeOffers(offers []session.TradeOffer)
	ViewAlwaysShowNameTag(entity world.Entity, alwaysShow bool)
	ViewBlockUpdate(pos cube.Pos, b world.Block, layer int)
	ViewEntityState(e world.Entity)
	ViewItemCooldown(item world.Item, duration time.Duration)
	ViewLayer() *world.ViewLayer
	ViewNameTag(entity world.Entity, nameTag string)
	ViewParticle(pos mgl64.Vec3, p world.Particle)
	ViewPublicAlwaysShowNameTag(entity world.Entity)
	ViewPublicNameTag(entity world.Entity)
	ViewPublicScoreTag(entity world.Entity)
	ViewScoreTag(entity world.Entity, scoreTag string)
	ViewSkin(e world.Entity)
	ViewSleepingPlayers(sleeping int, max int)
	ViewVisibility(entity world.Entity, level world.VisibilityLevel)
	VisibleDebugShapes() []debug.Shape
}

// Make sure the Bedrock session keeps satisfying Session.
var _ Session = (*session.Session)(nil)
