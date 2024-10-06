package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"io/fs"
	"log"
	"math"
	"math/rand"
	"time"

	"image/color"
	_ "image/png"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

//go:embed *
var assets embed.FS
var PlayerSprite = mustLoadImage("assets/player.png")
var LaserSprite = mustLoadImage("assets/laser.png")
var MeteorSprites = mustLoadImages("assets/meteors/*.png")
var ScoreFont = mustLoadFont("assets/font.ttf")
var LaserSound = mustLoadSound("assets/laser.ogg")

func mustLoadSound(name string) []byte {
	f, err := assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return f
}

func mustLoadFont(name string) *text.GoTextFaceSource {
	f, err := assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	s, err := text.NewGoTextFaceSource(bytes.NewReader(f))
	if err != nil {
		log.Fatal(err)
	}
	return s
}

const (
	shootCooldown         = time.Millisecond * 500
	rotationPerSecond     = math.Pi
	bulletSpawnOffset     = 50.0
	ScreenWidth           = 800
	ScreenHeight          = 600
	meteorSpawnTime       = 1 * time.Second
	meteorRandOffSet      = 250
	meteorRandOffSetAngle = 60
	bulletSpeedPerSecond  = 350.0
	sampleRate            = 48000
)

func mustLoadImage(name string) *ebiten.Image {
	f, err := assets.Open(name)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		panic(err)
	}

	return ebiten.NewImageFromImage(img)
}

func mustLoadImages(path string) []*ebiten.Image {
	matches, err := fs.Glob(assets, path)
	if err != nil {
		panic(err)
	}

	images := make([]*ebiten.Image, len(matches))
	for i, match := range matches {
		images[i] = mustLoadImage(match)
	}

	return images
}

type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

func NewRect(x, y, width, height float64) Rect {
	return Rect{
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
	}
}

func (r Rect) MaxX() float64 {
	return r.X + r.Width
}

func (r Rect) MaxY() float64 {
	return r.Y + r.Height
}

func (r Rect) Intersects(other Rect) bool {
	return r.X <= other.MaxX() &&
		other.X <= r.MaxX() &&
		r.Y <= other.MaxY() &&
		other.Y <= r.MaxY()
}

type Vector struct {
	X float64
	Y float64
}

func (v Vector) Normalize() Vector {
	magnitude := math.Sqrt(v.X*v.X + v.Y*v.Y)
	return Vector{v.X / magnitude, v.Y / magnitude}
}

type Player struct {
	game *Game

	position      Vector
	rotation      float64
	sprite        *ebiten.Image
	shootCooldown *Timer

	laserAudio       *audio.Context
	laserAudioPlayer *audio.Player
}

type Meteor struct {
	position      Vector
	movement      Vector
	rotation      float64
	rotationSpeed float64
	sprite        *ebiten.Image
}

func NewMeteor() *Meteor {
	sprite := MeteorSprites[rand.Intn(len(MeteorSprites))]

	// Figure out the target position — the screen center, in this case
	target := Vector{
		X: ScreenWidth/2 + float64(rand.Intn(meteorRandOffSet)) - float64(meteorRandOffSet)/2,
		Y: ScreenHeight/2 + float64(rand.Intn(meteorRandOffSet)) - float64(meteorRandOffSet)/2,
	}

	// The distance from the center the meteor should spawn at — half the width
	r := ScreenWidth / 2.0

	// Pick a random angle — 2π is 360° — so this returns 0° to 360°
	angle := rand.Float64()*2*math.Pi + float64(rand.Intn(meteorRandOffSetAngle)) - float64(meteorRandOffSetAngle)/2

	// Figure out the spawn position by moving r pixels from the target at the chosen angle
	pos := Vector{
		X: target.X + math.Cos(angle)*r,
		Y: target.Y + math.Sin(angle)*r,
	}

	// Randomized velocity
	velocity := 0.25 + rand.Float64()*1.5

	rotationSpeed := -0.02 + rand.Float64()*0.04

	// Direction is the target minus the current position
	direction := Vector{
		X: target.X - pos.X,
		Y: target.Y - pos.Y,
	}

	// Normalize the vector — get just the direction without the length
	normalizedDirection := direction.Normalize()

	// Multiply the direction by velocity
	movement := Vector{
		X: normalizedDirection.X * velocity,
		Y: normalizedDirection.Y * velocity,
	}

	return &Meteor{
		position:      pos,
		sprite:        sprite,
		movement:      movement,
		rotationSpeed: rotationSpeed,
	}
}

func (m *Meteor) Update() {
	m.position.X += m.movement.X
	m.position.Y += m.movement.Y
	m.rotation += m.rotationSpeed
}

func (m *Meteor) Draw(screen *ebiten.Image) {
	bounds := m.sprite.Bounds()
	halfW := float64(bounds.Dx()) / 2
	halfH := float64(bounds.Dy()) / 2

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-halfW, -halfH)
	op.GeoM.Rotate(m.rotation)
	op.GeoM.Translate(halfW, halfH)
	op.GeoM.Translate(m.position.X, m.position.Y)

	screen.DrawImage(m.sprite, op)
}

func (m *Meteor) Collider() Rect {
	bounds := m.sprite.Bounds()

	return NewRect(
		m.position.X,
		m.position.Y,
		float64(bounds.Dx()),
		float64(bounds.Dy()),
	)
}

func NewPlayer(game *Game) *Player {
	sprite := PlayerSprite

	bounds := sprite.Bounds()
	halfW := float64(bounds.Dx()) / 2
	halfH := float64(bounds.Dy()) / 2

	pos := Vector{
		X: ScreenWidth/2 - halfW,
		Y: ScreenHeight/2 - halfH,
	}

	audioContext := audio.NewContext(sampleRate)
	laserSound, _ := vorbis.DecodeF32(bytes.NewReader(LaserSound))
	player, _ := audioContext.NewPlayerF32(laserSound)
	return &Player{
		game:          game,
		position:      pos,
		sprite:        sprite,
		rotation:      0,
		shootCooldown: NewTimer(shootCooldown),

		laserAudio:       audioContext,
		laserAudioPlayer: player,
	}
}

func (p *Player) Update() {
	speed := math.Pi / float64(ebiten.TPS())

	if ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyA) {
		p.rotation -= speed
	}
	if ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyD) {
		p.rotation += speed
	}

	p.shootCooldown.Update()
	if p.shootCooldown.IsReady() && ebiten.IsKeyPressed(ebiten.KeySpace) {
		p.shootCooldown.Reset()

		bounds := p.sprite.Bounds()
		halfW := float64(bounds.Dx()) / 2
		halfH := float64(bounds.Dy()) / 2

		spawnPos := Vector{
			p.position.X + halfW + math.Sin(p.rotation)*bulletSpawnOffset,
			p.position.Y + halfH + math.Cos(p.rotation)*-bulletSpawnOffset,
		}

		bullet := NewBullet(spawnPos, p.rotation)
		p.game.AddBullet(bullet)
		p.laserAudioPlayer.SetPosition(0)
		p.laserAudioPlayer.Play()
	}
}

func (p *Player) Draw(screen *ebiten.Image) {
	bounds := p.sprite.Bounds()
	halfW := float64(bounds.Dx()) / 2
	halfH := float64(bounds.Dy()) / 2

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-halfW, -halfH)
	op.GeoM.Rotate(p.rotation)
	op.GeoM.Translate(halfW, halfH)

	op.GeoM.Translate(p.position.X, p.position.Y)

	screen.DrawImage(p.sprite, op)
}

func (p *Player) Collider() Rect {
	bounds := p.sprite.Bounds()

	return NewRect(
		p.position.X,
		p.position.Y,
		float64(bounds.Dx()),
		float64(bounds.Dy()),
	)
}

type Bullet struct {
	position Vector
	rotation float64
	sprite   *ebiten.Image
}

func NewBullet(pos Vector, rotation float64) *Bullet {
	bounds := LaserSprite.Bounds()
	halfW := float64(bounds.Dx()) / 2
	halfH := float64(bounds.Dy()) / 2

	pos.X -= halfW
	pos.Y -= halfH

	b := &Bullet{
		position: pos,
		rotation: rotation,
		sprite:   LaserSprite,
	}

	return b
}

func (b *Bullet) Update() {
	speed := bulletSpeedPerSecond / float64(ebiten.TPS())

	b.position.X += math.Sin(b.rotation) * speed
	b.position.Y += math.Cos(b.rotation) * -speed
}

func (b *Bullet) Draw(screen *ebiten.Image) {
	bounds := b.sprite.Bounds()
	halfW := float64(bounds.Dx()) / 2
	halfH := float64(bounds.Dy()) / 2

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-halfW, -halfH)
	op.GeoM.Rotate(b.rotation)
	op.GeoM.Translate(halfW, halfH)

	op.GeoM.Translate(b.position.X, b.position.Y)

	screen.DrawImage(b.sprite, op)
}

func (b *Bullet) Collider() Rect {
	bounds := b.sprite.Bounds()

	return NewRect(
		b.position.X,
		b.position.Y,
		float64(bounds.Dx()),
		float64(bounds.Dy()),
	)
}

type Game struct {
	player           *Player
	meteorSpawnTimer *Timer
	meteors          []*Meteor
	bullets          []*Bullet
	score            int
}

type Timer struct {
	currentTicks int
	targetTicks  int
}

func NewTimer(d time.Duration) *Timer {
	return &Timer{
		currentTicks: 0,
		targetTicks:  int(d.Milliseconds()) * ebiten.TPS() / 1000,
	}
}

func (t *Timer) Update() {
	if t.currentTicks < t.targetTicks {
		t.currentTicks++
	}
}

func (t *Timer) IsReady() bool {
	return t.currentTicks >= t.targetTicks
}

func (t *Timer) Reset() {
	t.currentTicks = 0
}

func (g *Game) Update() error {
	g.player.Update()

	g.meteorSpawnTimer.Update()
	if g.meteorSpawnTimer.IsReady() {
		g.meteorSpawnTimer.Reset()

		m := NewMeteor()
		g.meteors = append(g.meteors, m)
	}

	for _, m := range g.meteors {
		m.Update()
	}

	for _, b := range g.bullets {
		b.Update()
	}

	// Check for meteor/bullet collisions
	for i, m := range g.meteors {
		for j, b := range g.bullets {
			if m.Collider().Intersects(b.Collider()) {
				g.meteors = append(g.meteors[:i], g.meteors[i+1:]...)
				g.bullets = append(g.bullets[:j], g.bullets[j+1:]...)
				g.score++
			}
		}
	}

	// Check for meteor/player collisions
	for i, m := range g.meteors {
		if m.Collider().Intersects(g.player.Collider()) {
			g.meteors = append(g.meteors[:i], g.meteors[i+1:]...)
			g.score--
		}
	}

	g.score = max(g.score, 0)
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	outOfBoundsW := ScreenWidth * 1.5
	outOfBoundsH := ScreenHeight * 1.5
	g.player.Draw(screen)

	i := 0 // output index
	for _, m := range g.meteors {
		m.Draw(screen)
		if math.Abs(m.position.X) < outOfBoundsW && math.Abs(m.position.Y) < outOfBoundsH {
			g.meteors[i] = m
			i++
		}
	}
	g.meteors = g.meteors[:i]

	i = 0 // output index
	for _, b := range g.bullets {
		b.Draw(screen)
		if math.Abs(b.position.X) < outOfBoundsW && math.Abs(b.position.Y) < outOfBoundsH {
			g.bullets[i] = b
			i++
		}
	}
	g.bullets = g.bullets[:i]

	// Draw the sample text
	op := &text.DrawOptions{}
	op.GeoM.Translate(ScreenWidth/2-100, 50)
	op.ColorScale.ScaleWithColor(color.White)
	text.Draw(screen, fmt.Sprintf("%06d", g.score), &text.GoTextFace{
		Source: ScoreFont,
		Size:   48,
	}, op)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return ScreenWidth, ScreenHeight
}

func (g *Game) AddBullet(b *Bullet) {
	g.bullets = append(g.bullets, b)
}

func main() {
	g := &Game{meteorSpawnTimer: NewTimer(meteorSpawnTime)}
	g.player = NewPlayer(g)

	err := ebiten.RunGame(g)
	if err != nil {
		panic(err)
	}
}
