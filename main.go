package main

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path"
	"time"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/libretro/ludo/libretro"
)

type Game struct {
	message         string
	messageTimer    *time.Timer
	Width           int
	Height          int
	pixels          []byte
	PixelColors     []color.RGBA
	PixelFormat     uint32
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

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	corePath := path.Join(dir, os.Args[1])

	core, err := libretro.Load(corePath)
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

	gamePath := path.Join(dir, os.Args[2])

	core.SetEnvironment(func(env uint32, data unsafe.Pointer) bool {
		switch env {
		case
			libretro.EnvironmentGetVariable,
			libretro.EnvironmentGetVariableUpdate,
			libretro.EnvironmentSetSupportAchievements,
			libretro.EnvironmentGetInputBitmasks:
		default:
			log.Printf("RETRO_ENVIRONMENT: %v", env)
		}
		switch env {
		case libretro.EnvironmentSetPixelFormat:
			format := *(*uint32)(unsafe.Pointer(data))
			log.Printf("retro_set_environment - cmd=SET_PIXEL_FORMAT - fmt=%d\n", format)
			if format > libretro.PixelFormatRGB565 {
				log.Fatalf("invalid pixel format: %d", format)
			}
			game.PixelFormat = format
			return false
			// case libretro.EnvironmentGetVariable:
			// 	data2 := *(*retro_variable)(unsafe.Pointer(data))
			// 	_ = data2
			// 	// log.Printf("retro_set_environment - cmd=GET_VARIABLE - key=%#+v\n", charPtrToString(data2.key))
			// 	return false
			// case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE:
			// 	// log.Printf("retro_set_environment - cmd=GET_VARIABLE_UPDATE - result=%v\n", *(*bool)(unsafe.Pointer(data)))
			// 	return false
			// case RETRO_ENVIRONMENT_GET_LOG_INTERFACE:
			// 	log.Printf("retro_set_environment - cmd=GET_LOG_INTERFACE - data=%#+v\n", data)
			// 	return false
		}

		return false
	})

	core.SetInputPoll(func() {
		game.pressedKeys = inpututil.AppendPressedKeys(nil)
		game.justPressedKeys = inpututil.AppendJustPressedKeys(nil)
	})

	core.SetInputState(func(port uint, device uint32, index uint, id uint) int16 {
		if port != 0 || device != 1 || index != 0 {
			return 0
		}

		button := uint32(10000)
		for _, key := range game.pressedKeys {
			switch key {
			case ebiten.KeyArrowUp:
				button = libretro.DeviceIDJoypadUp
			case ebiten.KeyArrowDown:
				button = libretro.DeviceIDJoypadDown
			case ebiten.KeyArrowLeft:
				button = libretro.DeviceIDJoypadLeft
			case ebiten.KeyArrowRight:
				button = libretro.DeviceIDJoypadRight
			case ebiten.KeyC:
				button = libretro.DeviceIDJoypadB
			case ebiten.KeyX:
				button = libretro.DeviceIDJoypadA
			}
			if id == uint(button) {
				return 1
			}
		}

		return 0
	})

	// unsafe.Pointer, int32, int32, int32
	core.SetVideoRefresh(func(data unsafe.Pointer, width, height int32, pitch int32) {
		dataLen := width * height
		if len(game.PixelColors) != int(width*height) {
			game.PixelColors = make([]color.RGBA, int(width*height))
		}
		if game.Width != int(width) || game.Height != int(height) {
			game.Width = int(width)
			game.Height = int(height)
		}

		// log.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")

		for i := 0; i < int(dataLen); i++ {

			clr := color.RGBA{}

			switch game.PixelFormat {
			case libretro.PixelFormat0RGB1555:
				unitSize := unsafe.Sizeof(uint16(0))
				ptr := uintptr(data) + uintptr(i)*unitSize
				pixel := *(*uint16)(unsafe.Pointer(ptr))

				clr.A = byte(((pixel >> 15) & 1) * 255)
				clr.R = byte(((pixel >> 10) & 0b11111) * 255 / 31)
				clr.G = byte(((pixel >> 5) & 0b11111) * 255 / 31)
				clr.B = byte((pixel & 0b11111) * 255 / 31)

			case libretro.PixelFormatXRGB8888:
				unitSize := unsafe.Sizeof(uint32(0))
				ptr := uintptr(data) + uintptr(i)*unitSize
				pixel := *(*uint32)(unsafe.Pointer(ptr))

				clr.A = byte((pixel >> 24) & 0xFF)
				clr.R = byte((pixel >> 16) & 0xFF)
				clr.G = byte((pixel >> 8) & 0xFF)
				clr.B = byte(pixel & 0xFF)

				// TODO: RETRO_PIXEL_FORMAT_RGB565

			default:
				log.Fatalf("Pixel format not implemented: %d", game.PixelFormat)
			}

			game.PixelColors[i] = clr
		}
	})

	core.SetAudioSampleBatch(func(data []byte, frames int32) int32 {
		_ = audioWriter
		return 0
	})

	core.Init()

	sysInfo := core.GetSystemInfo()
	log.Printf("SYSTEM INFO: %s %s - NeedFullPath=%v - BlockExtract=%v - ValidExtensions=%v\n", sysInfo.LibraryName, sysInfo.LibraryVersion, sysInfo.NeedFullpath, sysInfo.BlockExtract, sysInfo.ValidExtensions)

	frameCount := 0
	lastKeyFrame := time.Now()
	fps := float64(0)

	game.DrawFunc = func(screen *ebiten.Image) {
		img := ebiten.NewImage(game.Width, game.Height)

		if len(game.pixels) != game.Width*game.Height*4 {
			game.pixels = make([]byte, game.Width*game.Height*4)
		}

		for i := 0; i < len(game.PixelColors); i++ {
			clr := game.PixelColors[i]
			copy(game.pixels[i*4:i*4+4], []byte{clr.R, clr.G, clr.B, clr.A})
		}
		game.pixels = append(game.pixels)

		img.WritePixels(game.pixels)
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
				state, err := core.Serialize(core.SerializeSize())
				if err != nil {
					log.Fatal(err)
				}
				err = os.WriteFile(statePath+fmt.Sprint(stateNumber), state, 0666)
				if err != nil {
					log.Fatal(err)
				}
				game.SetTempMessage(fmt.Sprintf("state saved at slot %d", stateNumber), time.Second)
				stateSaved = true
			case ebiten.KeyF4:
				b, err := os.ReadFile(statePath + fmt.Sprint(stateNumber))
				if err != nil {
					game.SetTempMessage(fmt.Sprintf("no save file in current slot: %d", stateNumber), time.Second)
					continue
				}
				err = core.Unserialize(b, core.SerializeSize())
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

	gameInfo := libretro.GameInfo{
		Path: gamePath,
	}
	if !sysInfo.NeedFullpath {
		b, err := os.ReadFile(gamePath)
		if err != nil {
			panic(err)
		}
		gameInfo.Data = unsafe.Pointer(unsafe.SliceData(b))
		gameInfo.Size = int64(len(b))
	}

	if ok := core.LoadGame(gameInfo); !ok {
		log.Fatal(err)
		return
	}

	avInfo := core.GetSystemAVInfo()

	log.Printf("AV INFO: Geometry=%+v - Timing=%+v\n", avInfo.Geometry, avInfo.Timing)
	audioCtx := audio.NewContext(int(avInfo.Timing.SampleRate))

	player, err := audioCtx.NewPlayerF32(audioReader)
	if err != nil {
		panic(err)
	}

	player.Play()

	err = ebiten.RunGame(game)
	if err != nil {
		log.Fatal(err)
	}
}
