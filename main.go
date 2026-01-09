package main

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/igorcafe/retro/libretro"
)

type Game struct {
	message         string
	messageTimer    *time.Timer
	Width           int
	Height          int
	Pixels          []byte
	pressedKeys     []ebiten.Key
	justPressedKeys []ebiten.Key

	DrawFunc   func(screen *ebiten.Image)
	LayoutFunc func(outsideWidth int, outsideHeight int) (screenWidth int, screenHeight int)
	UpdateFunc func() error
}

var _ ebiten.Game = &Game{}

func (g *Game) SetTempMessage(msg string, dur time.Duration) {
	g.message = msg
	if g.messageTimer != nil {
		g.messageTimer.Stop()
	}
	g.messageTimer = time.AfterFunc(dur, func() {
		g.message = ""
	})
}

// Draw implements ebiten.Game.
func (g *Game) Draw(screen *ebiten.Image) {
	g.DrawFunc(screen)
}

// Layout implements ebiten.Game.
func (g *Game) Layout(outsideWidth int, outsideHeight int) (screenWidth int, screenHeight int) {
	return g.LayoutFunc(outsideWidth, outsideHeight)
}

// Update implements ebiten.Game.
func (g *Game) Update() error {
	return g.UpdateFunc()
}

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("usage: %s CORE_PATH CONTENT_PATH", os.Args[0])
	}

	core := libretro.NewCore()
	// corePath := "./fceumm_libretro.so"
	corePath := os.Args[1]
	err := core.LoadDL(corePath)
	if err != nil {
		panic(err)
	}

	game := &Game{
		Width:  640,
		Height: 480,
	}

	audioReader, audioWriter := io.Pipe()
	statePath := os.Args[2] + ".state"
	stateNumber := 0
	stateSaved := false
	maxStates := 10

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	gamePath := path.Join(dir, os.Args[2])

	core.PollInput = func() {
		game.pressedKeys = inpututil.AppendPressedKeys(nil)
		game.justPressedKeys = inpututil.AppendJustPressedKeys(nil)
	}

	core.GetInputState = func(port, device, index, id uint) int16 {
		if port != 0 || device != 1 || index != 0 {
			return 0
		}

		button := uint(libretro.RETRO_DEVICE_ID_DUMMY)
		for _, key := range game.pressedKeys {
			switch key {
			case ebiten.KeyArrowUp:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_UP
			case ebiten.KeyArrowDown:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_DOWN
			case ebiten.KeyArrowLeft:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_LEFT
			case ebiten.KeyArrowRight:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_RIGHT
			case ebiten.KeyC:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_B
			case ebiten.KeyX:
				button = libretro.RETRO_DEVICE_ID_JOYPAD_A
			}
			if id == button {
				return 1
			}
		}

		return 0
	}

	core.OnLayoutChanged = func(width, height int) {
		game.Width = width
		game.Height = height
	}

	core.OnPixelsUpdate = func(pixels []color.RGBA) {
		if len(game.Pixels) != game.Width*game.Height*4 {
			game.Pixels = make([]byte, game.Width*game.Height*4)
		}

		for i := 0; i < len(pixels); i++ {
			clr := pixels[i]
			copy(game.Pixels[i*4:i*4+4], []byte{clr.R, clr.G, clr.B, clr.A})
		}
		game.Pixels = append(game.Pixels)
	}

	core.OnAudioSample = func(samples []int16) int {
		buf := make([]float32, len(samples))
		for i := range len(buf) {
			buf[i] = float32(samples[i]) / float32(1<<16)
		}
		_ = audioWriter
		// binary.Write(audioWriter, binary.LittleEndian, buf)
		return len(samples)
	}

	core.Init()

	sysInfo := core.GetSystemInfo()
	avInfo := core.GetSystemAVInfo()

	log.Printf("SYSTEM INFO: %s %s - NeedFullPath=%v - BlockExtract=%v - ValidExtensions=%v\n", sysInfo.LibraryName, sysInfo.LibraryVersion, sysInfo.NeedFullPath, sysInfo.BlockExtract, sysInfo.ValidExtensions)
	log.Printf("AV INFO: Geometry=%+v - Timing=%+v\n", avInfo.Geometry, avInfo.Timing)
	audioCtx := audio.NewContext(int(avInfo.Timing.SampleRate))

	player, err := audioCtx.NewPlayerF32(audioReader)
	if err != nil {
		panic(err)
	}

	player.Play()

	frameCount := 0
	lastKeyFrame := time.Now()
	fps := float64(0)

	game.DrawFunc = func(screen *ebiten.Image) {
		img := ebiten.NewImage(game.Width, game.Height)
		img.WritePixels(game.Pixels)
		screen.DrawImage(img, nil)

		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%.1f", fps), game.Width-30, 10)

		frameCount++
		if frameCount%60 == 0 {
			fps = 60 / time.Since(lastKeyFrame).Seconds()
			lastKeyFrame = time.Now()
		}

		if game.message != "" {
			ebitenutil.DebugPrintAt(screen, game.message, 10, 10)
		}
	}

	game.LayoutFunc = func(outsideWidth, outsideHeight int) (screenWidth int, screenHeight int) {
		return game.Width, game.Height
	}

	game.UpdateFunc = func() error {
		for _, key := range game.justPressedKeys {
			switch key {
			case ebiten.KeyF2:
				if stateSaved {
					stateNumber = (stateNumber + 1) % maxStates
				}
				f, err := os.Create(statePath + fmt.Sprint(stateNumber))
				if err != nil {
					log.Fatal(err)
				}
				err = core.WriteState(f)
				if err != nil {
					log.Fatal(err)
				}
				err = f.Close()
				if err != nil {
					log.Fatal(err)
				}
				game.SetTempMessage(fmt.Sprintf("state saved at slot %d", stateNumber), time.Second)
				stateSaved = true
			case ebiten.KeyF4:
				f, err := os.Open(statePath + fmt.Sprint(stateNumber))
				if err != nil {
					game.SetTempMessage(fmt.Sprintf("no save file in current slot: %d", stateNumber), time.Second)
					continue
				}
				err = core.ReadState(f)
				if err != nil {
					log.Fatal(err)
				}
				err = f.Close()
				if err != nil {
					log.Fatal(err)
				}
				game.SetTempMessage(fmt.Sprintf("state loaded from slot %d", stateNumber), time.Second)
			case ebiten.KeyF6:
				stateSaved = false
				stateNumber = (stateNumber + maxStates - 1) % maxStates
				game.SetTempMessage(fmt.Sprintf("current slot: %d", stateNumber), time.Second)
			case ebiten.KeyF7:
				stateSaved = false
				stateNumber = (stateNumber + 1) % maxStates
				game.SetTempMessage(fmt.Sprintf("current slot: %d", stateNumber), time.Second)
			}
		}

		core.Run()
		return nil
	}

	gameInfo := libretro.GameInfo{}
	if sysInfo.NeedFullPath {
		gameInfo = libretro.GameInfo{Path: gamePath}
	} else {
		b, err := os.ReadFile(gamePath)
		if err != nil {
			panic(err)
		}
		gameInfo = libretro.GameInfo{
			Data: b,
			Size: uint64(len(b)),
		}
	}

	if err := core.LoadGame(gameInfo); err != nil {
		log.Fatal(err)
		return
	}

	err = ebiten.RunGame(game)
	if err != nil {
		log.Fatal(err)
	}
}
