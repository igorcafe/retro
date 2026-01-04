package libretro

import (
	"fmt"
	"image/color"
	"log"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

type Core struct {
	width       int
	height      int
	pixels      []color.RGBA
	pixelFormat PixelFormat

	OnPixelsUpdate  func(pixels []color.RGBA)
	OnLayoutChanged func(width, height int)
	OnAudioSample   func(data []int16) int
	PollInput       func()
	GetInputState   func(port, device, index, id uint) int16

	// core dynamic library handle and functions
	handle                           uintptr
	retro_set_environment            func(func(cmd uint, data uintptr) bool)
	retro_set_video_refresh          func(func(data uintptr, width uint, height uint, pitch uint64))
	retro_set_audio_sample           func(func(left, right int16))
	retro_set_audio_sample_batch     func(func(data *int16, frames uint64) uint64)
	retro_set_input_poll             func(func())
	retro_set_input_state            func(func(port uint, device uint, index uint, id uint) int16)
	retro_init                       func()
	retro_deinit                     func()
	retro_api_version                func() uint
	retro_load_game                  func(*retro_game_info) bool
	retro_unload_game                func()
	retro_reset                      func()
	retro_run                        func()
	retro_serialize_size             func() uint64
	retro_get_system_info            func(*retro_system_info)
	retro_get_system_av_info         func(*retro_system_av_info)
	retro_set_controller_port_device func(port, device uint)
	retro_serialize                  func(data uintptr, size uint64)
	retro_unserialize                func(data uintptr, size uint64)

	// callbacks
	callback_log func(level retro_log_level, fmt *byte, args uintptr)
}

func NewCore() *Core {
	return &Core{
		OnLayoutChanged: func(width, height int) {},
		OnPixelsUpdate:  func(pixels []color.RGBA) {},
		PollInput:       func() {},
		OnAudioSample:   func(data []int16) int { return 0 },
		GetInputState:   func(port, device, index, id uint) int16 { return 0 },
	}
}

func (c *Core) LoadDL(path string) error {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}

	c.handle = handle

	// void cb_c_log(enum retro_log_level level, const char *fmt, ...)

	c.callback_log = func(level retro_log_level, format *byte, args uintptr) {
		formatStr := charPtrToString(format)
		log.Println("callback_log", retroLogLevelString(level), formatStr, args)
	}

	purego.RegisterLibFunc(&c.retro_init, handle, "retro_init")
	purego.RegisterLibFunc(&c.retro_deinit, handle, "retro_deinit")
	purego.RegisterLibFunc(&c.retro_api_version, handle, "retro_api_version")
	purego.RegisterLibFunc(&c.retro_set_environment, handle, "retro_set_environment")
	purego.RegisterLibFunc(&c.retro_set_video_refresh, handle, "retro_set_video_refresh")
	purego.RegisterLibFunc(&c.retro_set_audio_sample, handle, "retro_set_audio_sample")
	purego.RegisterLibFunc(&c.retro_set_audio_sample_batch, handle, "retro_set_audio_sample_batch")
	purego.RegisterLibFunc(&c.retro_set_input_poll, handle, "retro_set_input_poll")
	purego.RegisterLibFunc(&c.retro_set_input_state, handle, "retro_set_input_state")
	purego.RegisterLibFunc(&c.retro_get_system_info, handle, "retro_get_system_info")
	purego.RegisterLibFunc(&c.retro_get_system_av_info, handle, "retro_get_system_av_info")
	purego.RegisterLibFunc(&c.retro_set_controller_port_device, handle, "retro_set_controller_port_device")
	purego.RegisterLibFunc(&c.retro_load_game, handle, "retro_load_game")
	purego.RegisterLibFunc(&c.retro_unload_game, handle, "retro_unload_game")
	purego.RegisterLibFunc(&c.retro_reset, handle, "retro_reset")
	purego.RegisterLibFunc(&c.retro_run, handle, "retro_run")
	purego.RegisterLibFunc(&c.retro_serialize_size, handle, "retro_serialize_size")
	purego.RegisterLibFunc(&c.retro_serialize, handle, "retro_serialize")
	purego.RegisterLibFunc(&c.retro_unserialize, handle, "retro_unserialize")

	c.retro_set_environment(func(cmd uint, data uintptr) bool {
		env := RETRO_ENVIRONMENT(cmd)
		switch env {
		case
			RETRO_ENVIRONMENT_GET_VARIABLE,
			RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE,
			RETRO_ENVIRONMENT_SET_SUPPORT_ACHIEVEMENTS,
			RETRO_ENVIRONMENT_GET_INPUT_BITMASKS:
		default:
			if env&RETRO_ENVIRONMENT_EXPERIMENTAL == 1 {
				return false
			}
			log.Printf("RETRO_ENVIRONMENT: %s", env.String())
		}
		switch env {
		case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT:
			format := *(*PixelFormat)(unsafe.Pointer(data))
			log.Printf("retro_set_environment - cmd=SET_PIXEL_FORMAT - fmt=%d\n", format)
			if format >= PixelFormatInvalid {
				log.Fatalf("invalid pixel format: %d", format)
			}
			c.pixelFormat = format
			return false
		case RETRO_ENVIRONMENT_GET_VARIABLE:
			data2 := *(*retro_variable)(unsafe.Pointer(data))
			_ = data2
			// log.Printf("retro_set_environment - cmd=GET_VARIABLE - key=%#+v\n", charPtrToString(data2.key))
			return false
		case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE:
			// log.Printf("retro_set_environment - cmd=GET_VARIABLE_UPDATE - result=%v\n", *(*bool)(unsafe.Pointer(data)))
			return false
		case RETRO_ENVIRONMENT_GET_LOG_INTERFACE:
			log.Printf("retro_set_environment - cmd=GET_LOG_INTERFACE - data=%#+v\n", data)
			return false
		}

		return false
	})

	c.retro_set_video_refresh(func(data uintptr, width, height uint, pitch uint64) {
		// log.Println("video_refresh", width, height, pitch)
		dataLen := width * height
		if len(c.pixels) != int(width*height) {
			c.pixels = make([]color.RGBA, int(width*height))
		}
		if c.width != int(width) || c.height != int(height) {
			c.width = int(width)
			c.height = int(height)
			c.OnLayoutChanged(int(width), int(height))
		}

		// log.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")

		for i := 0; i < int(dataLen); i++ {

			clr := color.RGBA{}

			switch c.pixelFormat {
			case PixelFormat0RGB1555:
				unitSize := unsafe.Sizeof(uint16(0))
				ptr := data + uintptr(i)*unitSize
				pixel := *(*uint16)(unsafe.Pointer(ptr))

				clr.A = byte(((pixel >> 15) & 1) * 255)
				clr.R = byte(((pixel >> 10) & 0b11111) * 255 / 31)
				clr.G = byte(((pixel >> 5) & 0b11111) * 255 / 31)
				clr.B = byte((pixel & 0b11111) * 255 / 31)

			case PixelFormatXRGB8888:
				unitSize := unsafe.Sizeof(uint32(0))
				ptr := data + uintptr(i)*unitSize
				pixel := *(*uint32)(unsafe.Pointer(ptr))

				clr.A = byte((pixel >> 24) & 0xFF)
				clr.R = byte((pixel >> 16) & 0xFF)
				clr.G = byte((pixel >> 8) & 0xFF)
				clr.B = byte(pixel & 0xFF)

				// TODO: RETRO_PIXEL_FORMAT_RGB565

			default:
				log.Fatalf("Pixel format not implemented: %d", c.pixelFormat)
			}

			c.pixels[i] = clr
		}
		c.OnPixelsUpdate(c.pixels)
	})

	c.retro_set_audio_sample(func(left, right int16) {
		c.OnAudioSample([]int16{left, right})
	})

	c.retro_set_audio_sample_batch(func(data *int16, frames uint64) uint64 {
		samples := unsafe.Slice(data, frames*2)
		return uint64(c.OnAudioSample(samples))
	})

	c.retro_set_input_poll(func() {
		c.PollInput()
	})

	c.retro_set_input_state(func(port, device, index, id uint) int16 {
		return c.GetInputState(port, device, index, id)
	})

	return nil
}

func (c *Core) Init() {
	c.retro_init()
}

func (c *Core) Deinit() {
	c.retro_deinit()
}

func (c *Core) Run() {
	c.retro_run()
}

func (c *Core) GetSystemInfo() SystemInfo {
	info := retro_system_info{}
	c.retro_get_system_info(&info)

	return SystemInfo{
		LibraryName:     charPtrToString(info.library_name),
		LibraryVersion:  charPtrToString(info.library_version),
		ValidExtensions: strings.Split(charPtrToString(info.valid_extensions), "|"),
		NeedFullPath:    info.need_fullpath,
		BlockExtract:    info.block_extract,
	}
}

type GameGeometry struct {
	BaseWidth  uint32 /* Nominal video width of game. */
	BaseHeight uint32 /* Nominal video height of game. */
	MaxWidth   uint32 /* Maximum possible width of game. */
	MaxHeight  uint32 /* Maximum possible height of game. */

	// Nominal aspect ratio of game. If
	// aspect_ratio is <= 0.0, an aspect ratio
	// of base_width / base_height is assumed.
	// A frontend could override this setting,
	// if desired.
	AspectRatio float32
}

type SystemTime struct {
	// FPS of video content.
	FPS float64

	// Sampling rate of audio.
	SampleRate float64
}

type SystemAVInfo struct {
	Geometry GameGeometry
	Timing   SystemTime
}

func (c *Core) GetSystemAVInfo() SystemAVInfo {
	av_info := retro_system_av_info{}
	c.retro_get_system_av_info(&av_info)

	return SystemAVInfo{
		Geometry: GameGeometry{
			BaseWidth:   av_info.geometry.base_width,
			BaseHeight:  av_info.geometry.base_height,
			MaxWidth:    av_info.geometry.max_width,
			MaxHeight:   av_info.geometry.max_height,
			AspectRatio: av_info.geometry.aspect_ratio,
		},
		Timing: SystemTime{
			FPS:        av_info.timing.fps,
			SampleRate: 44100, // av_info.timing.sample_rate
		},
	}
}

type GameInfo struct {
	Path string
	Data []byte
	Size uint64
	Meta string
}

func (c *Core) LoadGame(info GameInfo) error {
	retro_info := retro_game_info{
		path: stringToCharPtr(info.Path),
		data: (*byte)(unsafe.Pointer(unsafe.SliceData(info.Data))),
		size: uint64(len(info.Data)),
	}
	if ok := c.retro_load_game(&retro_info); !ok {
		return fmt.Errorf("failed to load game %+v", retro_info)
	}
	return nil
}

type SystemInfo struct {
	LibraryName     string
	LibraryVersion  string
	ValidExtensions []string
	NeedFullPath    bool
	BlockExtract    bool
}

type retro_log_level int

const (
	RETRO_LOG_DEBUG retro_log_level = iota
	RETRO_LOG_INFO
	RETRO_LOG_WARN
	RETRO_LOG_ERROR
	// RETRO_LOG_DUMMY =
)

func retroLogLevelString(level retro_log_level) string {
	switch level {
	case RETRO_LOG_DEBUG:
		return "DEBUG"
	case RETRO_LOG_INFO:
		return "INFO"
	case RETRO_LOG_WARN:
		return "WARN"
	case RETRO_LOG_ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

type retro_variable struct {
	// Variable to query in RETRO_ENVIRONMENT_GET_VARIABLE.
	// If NULL, obtains the complete environment string if more
	// complex parsing is necessary.
	// The environment string is formatted as key-value pairs
	// delimited by semicolons as so:
	// "key1=value1;key2=value2;..."
	key *byte

	// Value to be obtained. If key does not exist, it is set to NULL.
	value *byte
}

type retro_game_info struct {
	path *byte
	data *byte
	size uint64
	meta *byte
}

type retro_system_info struct {
	//  Descriptive name of library. Should not
	//  contain any version numbers, etc.
	library_name *byte

	// Descriptive version of core.
	library_version *byte

	// A string listing probably content
	// extensions the core will be able to
	// load, separated with pipe.
	// I.e. "bin|rom|iso".
	// Typically used for a GUI to filter
	// out extensions.
	valid_extensions *byte

	// Libretro cores that need to have direct access to their content
	// files, including cores which use the path of the content files to
	// determine the paths of other files, should set need_fullpath to true.
	//
	// Cores should strive for setting need_fullpath to false,
	// as it allows the frontend to perform patching, etc.
	//
	// If need_fullpath is true and retro_load_game() is called:
	//    - retro_game_info::path is guaranteed to have a valid path
	//    - retro_game_info::data and retro_game_info::size are invalid
	//
	// If need_fullpath is false and retro_load_game() is called:
	//    - retro_game_info::path may be NULL
	//    - retro_game_info::data and retro_game_info::size are guaranteed
	//      to be valid
	//
	// See also:
	//    - RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY
	//    - RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY
	//
	need_fullpath bool

	// If true, the frontend is not allowed to extract any archives before
	// loading the real content.
	// Necessary for certain libretro implementations that load games
	// from zipped archives.
	block_extract bool
}

type retro_system_av_info struct {
	geometry retro_game_geometry
	timing   retro_system_timing
}

type retro_game_geometry struct {
	// Nominal video width of game.
	base_width uint32

	// Nominal video height of game.
	base_height uint32

	// Maximum possible width of game.
	max_width uint32

	// Maximum possible height of game.
	max_height uint32

	// Nominal aspect ratio of game. If
	// aspect_ratio is <= 0.0, an aspect ratio
	// of base_width / base_height is assumed.
	// A frontend could override this setting,
	// if desired.
	aspect_ratio float32
}

type retro_system_timing struct {
	fps         float64 /* FPS of video content. */
	sample_rate float64 /* Sampling rate of audio. */
}

//go:generate stringer -type=RETRO_ENVIRONMENT
type RETRO_ENVIRONMENT int32

const (
	RETRO_ENVIRONMENT_EXPERIMENTAL                                        RETRO_ENVIRONMENT = 0x10000
	RETRO_ENVIRONMENT_SET_ROTATION                                        RETRO_ENVIRONMENT = 1
	RETRO_ENVIRONMENT_GET_OVERSCAN                                        RETRO_ENVIRONMENT = 2
	RETRO_ENVIRONMENT_GET_CAN_DUPE                                        RETRO_ENVIRONMENT = 3
	RETRO_ENVIRONMENT_SET_MESSAGE                                         RETRO_ENVIRONMENT = 6
	RETRO_ENVIRONMENT_SHUTDOWN                                            RETRO_ENVIRONMENT = 7
	RETRO_ENVIRONMENT_SET_PERFORMANCE_LEVEL                               RETRO_ENVIRONMENT = 8
	RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY                                RETRO_ENVIRONMENT = 9
	RETRO_ENVIRONMENT_SET_PIXEL_FORMAT                                    RETRO_ENVIRONMENT = 10
	RETRO_ENVIRONMENT_SET_INPUT_DESCRIPTORS                               RETRO_ENVIRONMENT = 11
	RETRO_ENVIRONMENT_SET_KEYBOARD_CALLBACK                               RETRO_ENVIRONMENT = 12
	RETRO_ENVIRONMENT_SET_DISK_CONTROL_INTERFACE                          RETRO_ENVIRONMENT = 13
	RETRO_ENVIRONMENT_SET_HW_RENDER                                       RETRO_ENVIRONMENT = 14
	RETRO_ENVIRONMENT_GET_VARIABLE                                        RETRO_ENVIRONMENT = 15
	RETRO_ENVIRONMENT_SET_VARIABLES                                       RETRO_ENVIRONMENT = 16
	RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE                                 RETRO_ENVIRONMENT = 17
	RETRO_ENVIRONMENT_SET_SUPPORT_NO_GAME                                 RETRO_ENVIRONMENT = 18
	RETRO_ENVIRONMENT_GET_LIBRETRO_PATH                                   RETRO_ENVIRONMENT = 19
	RETRO_ENVIRONMENT_SET_FRAME_TIME_CALLBACK                             RETRO_ENVIRONMENT = 21
	RETRO_ENVIRONMENT_SET_AUDIO_CALLBACK                                  RETRO_ENVIRONMENT = 22
	RETRO_ENVIRONMENT_GET_RUMBLE_INTERFACE                                RETRO_ENVIRONMENT = 23
	RETRO_ENVIRONMENT_GET_INPUT_DEVICE_CAPABILITIES                       RETRO_ENVIRONMENT = 24
	RETRO_ENVIRONMENT_GET_SENSOR_INTERFACE                                RETRO_ENVIRONMENT = (25 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_CAMERA_INTERFACE                                RETRO_ENVIRONMENT = (26 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_LOG_INTERFACE                                   RETRO_ENVIRONMENT = 27
	RETRO_ENVIRONMENT_GET_PERF_INTERFACE                                  RETRO_ENVIRONMENT = 28
	RETRO_ENVIRONMENT_GET_LOCATION_INTERFACE                              RETRO_ENVIRONMENT = 29
	RETRO_ENVIRONMENT_GET_CONTENT_DIRECTORY                               RETRO_ENVIRONMENT = 30
	RETRO_ENVIRONMENT_GET_CORE_ASSETS_DIRECTORY                           RETRO_ENVIRONMENT = 30
	RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY                                  RETRO_ENVIRONMENT = 31
	RETRO_ENVIRONMENT_SET_SYSTEM_AV_INFO                                  RETRO_ENVIRONMENT = 32
	RETRO_ENVIRONMENT_SET_PROC_ADDRESS_CALLBACK                           RETRO_ENVIRONMENT = 33
	RETRO_ENVIRONMENT_SET_SUBSYSTEM_INFO                                  RETRO_ENVIRONMENT = 34
	RETRO_ENVIRONMENT_SET_CONTROLLER_INFO                                 RETRO_ENVIRONMENT = 35
	RETRO_ENVIRONMENT_SET_MEMORY_MAPS                                     RETRO_ENVIRONMENT = (36 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_SET_GEOMETRY                                        RETRO_ENVIRONMENT = 37
	RETRO_ENVIRONMENT_GET_USERNAME                                        RETRO_ENVIRONMENT = 38
	RETRO_ENVIRONMENT_GET_LANGUAGE                                        RETRO_ENVIRONMENT = 39
	RETRO_ENVIRONMENT_GET_CURRENT_SOFTWARE_FRAMEBUFFER                    RETRO_ENVIRONMENT = (40 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_HW_RENDER_INTERFACE                             RETRO_ENVIRONMENT = (41 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_SET_SUPPORT_ACHIEVEMENTS                            RETRO_ENVIRONMENT = (42 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_SET_HW_RENDER_CONTEXT_NEGOTIATION_INTERFACE         RETRO_ENVIRONMENT = (43 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_SET_SERIALIZATION_QUIRKS                            RETRO_ENVIRONMENT = 44
	RETRO_ENVIRONMENT_SET_HW_SHARED_CONTEXT                               RETRO_ENVIRONMENT = (44 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_VFS_INTERFACE                                   RETRO_ENVIRONMENT = (45 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_LED_INTERFACE                                   RETRO_ENVIRONMENT = (46 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_AUDIO_VIDEO_ENABLE                              RETRO_ENVIRONMENT = (47 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_MIDI_INTERFACE                                  RETRO_ENVIRONMENT = (48 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_FASTFORWARDING                                  RETRO_ENVIRONMENT = (49 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_TARGET_REFRESH_RATE                             RETRO_ENVIRONMENT = (50 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_INPUT_BITMASKS                                  RETRO_ENVIRONMENT = (51 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_CORE_OPTIONS_VERSION                            RETRO_ENVIRONMENT = 52
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS                                    RETRO_ENVIRONMENT = 53
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS_INTL                               RETRO_ENVIRONMENT = 54
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS_DISPLAY                            RETRO_ENVIRONMENT = 55
	RETRO_ENVIRONMENT_GET_PREFERRED_HW_RENDER                             RETRO_ENVIRONMENT = 56
	RETRO_ENVIRONMENT_GET_DISK_CONTROL_INTERFACE_VERSION                  RETRO_ENVIRONMENT = 57
	RETRO_ENVIRONMENT_SET_DISK_CONTROL_EXT_INTERFACE                      RETRO_ENVIRONMENT = 58
	RETRO_ENVIRONMENT_GET_MESSAGE_INTERFACE_VERSION                       RETRO_ENVIRONMENT = 59
	RETRO_ENVIRONMENT_SET_MESSAGE_EXT                                     RETRO_ENVIRONMENT = 60
	RETRO_ENVIRONMENT_GET_INPUT_MAX_USERS                                 RETRO_ENVIRONMENT = 61
	RETRO_ENVIRONMENT_SET_AUDIO_BUFFER_STATUS_CALLBACK                    RETRO_ENVIRONMENT = 62
	RETRO_ENVIRONMENT_SET_MINIMUM_AUDIO_LATENCY                           RETRO_ENVIRONMENT = 63
	RETRO_ENVIRONMENT_SET_FASTFORWARDING_OVERRIDE                         RETRO_ENVIRONMENT = 64
	RETRO_ENVIRONMENT_SET_CONTENT_INFO_OVERRIDE                           RETRO_ENVIRONMENT = 65
	RETRO_ENVIRONMENT_GET_GAME_INFO_EXT                                   RETRO_ENVIRONMENT = 66
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS_V2                                 RETRO_ENVIRONMENT = 67
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS_V2_INTL                            RETRO_ENVIRONMENT = 68
	RETRO_ENVIRONMENT_SET_CORE_OPTIONS_UPDATE_DISPLAY_CALLBACK            RETRO_ENVIRONMENT = 69
	RETRO_ENVIRONMENT_SET_VARIABLE                                        RETRO_ENVIRONMENT = 70
	RETRO_ENVIRONMENT_GET_THROTTLE_STATE                                  RETRO_ENVIRONMENT = (71 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_SAVESTATE_CONTEXT                               RETRO_ENVIRONMENT = (72 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_HW_RENDER_CONTEXT_NEGOTIATION_INTERFACE_SUPPORT RETRO_ENVIRONMENT = (73 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_GET_JIT_CAPABLE                                     RETRO_ENVIRONMENT = 74
	RETRO_ENVIRONMENT_GET_MICROPHONE_INTERFACE                            RETRO_ENVIRONMENT = (75 | RETRO_ENVIRONMENT_EXPERIMENTAL)
	RETRO_ENVIRONMENT_SET_NETPACKET_INTERFACE                             RETRO_ENVIRONMENT = 76
	RETRO_ENVIRONMENT_GET_DEVICE_POWER                                    RETRO_ENVIRONMENT = (77 | RETRO_ENVIRONMENT_EXPERIMENTAL)
)

const (
	RETRO_DEVICE_NONE     = 0
	RETRO_DEVICE_JOYPAD   = 1
	RETRO_DEVICE_MOUSE    = 2
	RETRO_DEVICE_KEYBOARD = 3
	RETRO_DEVICE_LIGHTGUN = 4
	RETRO_DEVICE_ANALOG   = 5
	RETRO_DEVICE_POINTER  = 6
)

const (
	RETRO_DEVICE_ID_JOYPAD_B      = 0
	RETRO_DEVICE_ID_JOYPAD_Y      = 1
	RETRO_DEVICE_ID_JOYPAD_SELECT = 2
	RETRO_DEVICE_ID_JOYPAD_START  = 3
	RETRO_DEVICE_ID_JOYPAD_UP     = 4
	RETRO_DEVICE_ID_JOYPAD_DOWN   = 5
	RETRO_DEVICE_ID_JOYPAD_LEFT   = 6
	RETRO_DEVICE_ID_JOYPAD_RIGHT  = 7
	RETRO_DEVICE_ID_JOYPAD_A      = 8
	RETRO_DEVICE_ID_JOYPAD_X      = 9
	RETRO_DEVICE_ID_JOYPAD_L      = 10
	RETRO_DEVICE_ID_JOYPAD_R      = 11
	RETRO_DEVICE_ID_JOYPAD_L2     = 12
	RETRO_DEVICE_ID_JOYPAD_R2     = 13
	RETRO_DEVICE_ID_JOYPAD_L3     = 14
	RETRO_DEVICE_ID_JOYPAD_R3     = 15
	RETRO_DEVICE_ID_DUMMY         = 1000
)

type retro_log_callback struct {
	log uintptr
}

func charPtrToString(bytePtr *byte) string {
	if bytePtr == nil {
		return ""
	}
	ptr := uintptr(unsafe.Pointer(bytePtr))
	builder := &strings.Builder{}
	for i := 0; ; i++ {
		b := *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
		if b == '\000' {
			break
		}
		builder.WriteByte(b)
	}
	return builder.String()
}

func stringToCharPtr(s string) *byte {
	return (*byte)(unsafe.Pointer(unsafe.StringData(s + "\000")))
}

type PixelFormat int32

const (
	/* 0RGB1555, native endian.
	 * 0 bit must be set to 0.
	 * This pixel format is default for compatibility concerns only.
	 * If a 15/16-bit pixel format is desired, consider using RGB565. */
	PixelFormat0RGB1555 PixelFormat = 0

	/* XRGB8888, native endian.
	 * X bits are ignored. */
	PixelFormatXRGB8888 PixelFormat = 1

	/* RGB565, native endian.
	 * This pixel format is the recommended format to use if a 15/16-bit
	 * format is desired as it is the pixel format that is typically
	 * available on a wide range of low-power devices.
	 *
	 * It is also natively supported in APIs like OpenGL ES. */
	PixelFormatRGB565 PixelFormat = 2

	PixelFormatInvalid PixelFormat = 3
)
