// paddlekeys: turn a USB CW paddle adapter that shows up as a mouse into
// keyboard presses, so it can drive VBand (which wants [ and ]).
//
// It grabs the adapter's evdev node exclusively (so the desktop never sees
// the mouse clicks) and re-emits each button press/release as a key event
// through a virtual keyboard on /dev/uinput.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// ioctl numbers from <linux/input.h> and <linux/uinput.h> (x86_64).
const (
	eviocGrab    = 0x40044590 // _IOW('E', 0x90, int)
	eviocGName   = 0x81004506 // _IOC(_IOC_READ, 'E', 0x06, 256)
	eviocGID     = 0x80084502 // _IOR('E', 0x02, struct input_id)
	uiSetEvBit   = 0x40045564 // _IOW('U', 100, int)
	uiSetKeyBit  = 0x40045565 // _IOW('U', 101, int)
	uiDevSetup   = 0x405c5503 // _IOW('U', 3, struct uinput_setup)
	uiDevCreate  = 0x5501     // _IO('U', 1)
	uiDevDestroy = 0x5502     // _IO('U', 2)
)

// Event types and codes from <linux/input-event-codes.h>.
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evMsc = 0x04

	synReport = 0

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
)

// inputEvent mirrors struct input_event on 64-bit Linux (24 bytes).
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

// uinputSetup mirrors struct uinput_setup (92 bytes).
type uinputSetup struct {
	Bustype, Vendor, Product, Version uint16
	Name                              [80]byte
	FFEffectsMax                      uint32
}

// keyNames maps friendly names to evdev key codes.
var keyNames = map[string]uint16{
	"[": 26, "leftbrace": 26, "lbracket": 26,
	"]": 27, "rightbrace": 27, "rbracket": 27,
	"leftctrl": 29, "lctrl": 29, "ctrl": 29,
	"rightctrl": 97, "rctrl": 97,
	"leftshift": 42, "rightshift": 54,
	"leftalt": 56, "rightalt": 100,
	"space": 57, "enter": 28, "tab": 15, "esc": 1,
	"comma": 51, ",": 51, "dot": 52, ".": 52, "slash": 53, "/": 53,
	"semicolon": 39, ";": 39, "apostrophe": 40, "'": 40,
	"minus": 12, "-": 12, "equal": 13, "=": 13,
	"up": 103, "down": 108, "left": 105, "right": 106,
	"f1": 59, "f2": 60, "f3": 61, "f4": 62, "f5": 63, "f6": 64,
	"f7": 65, "f8": 66, "f9": 67, "f10": 68, "f11": 87, "f12": 88,
}

func init() {
	// Letters and digits.
	letters := "qwertyuiop"
	for i, c := range letters {
		keyNames[string(c)] = uint16(16 + i)
	}
	for i, c := range "asdfghjkl" {
		keyNames[string(c)] = uint16(30 + i)
	}
	for i, c := range "zxcvbnm" {
		keyNames[string(c)] = uint16(44 + i)
	}
	for i, c := range "1234567890" {
		keyNames[string(c)] = uint16(2 + i)
	}
}

var buttonNames = map[uint16]string{btnLeft: "BTN_LEFT", btnRight: "BTN_RIGHT", btnMiddle: "BTN_MIDDLE"}

func lookupKey(name string) (uint16, error) {
	if code, ok := keyNames[strings.ToLower(name)]; ok {
		return code, nil
	}
	var n uint16
	if _, err := fmt.Sscanf(name, "%d", &n); err == nil {
		return n, nil
	}
	return 0, fmt.Errorf("unknown key %q (try -list-keys)", name)
}

func ioctl(fd int, req uint, arg uintptr) error {
	_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), arg)
	if e != 0 {
		return e
	}
	return nil
}

func deviceName(fd int) string {
	var buf [256]byte
	if err := ioctl(fd, eviocGName, uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		return "?"
	}
	return unix.ByteSliceToString(buf[:])
}

// deviceID returns the vendor and product IDs of an evdev device.
func deviceID(fd int) (vendor, product uint16) {
	var id struct{ Bustype, Vendor, Product, Version uint16 }
	if err := ioctl(fd, eviocGID, uintptr(unsafe.Pointer(&id))); err != nil {
		return 0, 0
	}
	return id.Vendor, id.Product
}

// parseUSBID parses "vvvv:pppp" hex into vendor and product IDs.
func parseUSBID(s string) (uint16, uint16, error) {
	var v, p uint16
	if _, err := fmt.Sscanf(s, "%4x:%4x", &v, &p); err != nil {
		return 0, 0, fmt.Errorf("bad USB id %q, want vvvv:pppp", s)
	}
	return v, p, nil
}

// findDevice looks for an evdev node that reports mouse buttons and whose
// name contains nameMatch (if non-empty) or whose USB vendor:product equals
// vid:pid (if non-zero). Returns the first match.
func findDevice(nameMatch string, vid, pid uint16) (string, error) {
	paths, _ := filepath.Glob("/dev/input/event*")
	sort.Strings(paths)
	for _, p := range paths {
		fd, err := unix.Open(p, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		name := deviceName(fd)
		v, pr := deviceID(fd)
		hasButtons := supportsKey(fd, btnLeft)
		unix.Close(fd)
		if !hasButtons {
			continue
		}
		byName := nameMatch != "" && strings.Contains(strings.ToLower(name), strings.ToLower(nameMatch))
		byID := vid != 0 && v == vid && pr == pid
		if byName || byID {
			return p, nil
		}
	}
	return "", fmt.Errorf("no evdev device with mouse buttons matching name %q or USB id %04x:%04x found", nameMatch, vid, pid)
}

// envOr returns the environment variable PADDLEKEYS_<key> if set, else def.
// This lets /etc/default/paddlekeys (loaded by the systemd unit) configure
// the service without editing the unit.
func envOr(key, def string) string {
	if v, ok := os.LookupEnv("PADDLEKEYS_" + key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(envOr(key, ""))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// supportsKey asks the device whether it can emit the given EV_KEY code
// via EVIOCGBIT(EV_KEY, len).
func supportsKey(fd int, code uint16) bool {
	var bits [96]byte // KEY_MAX (0x2ff) / 8 rounded up
	req := uint(0x80000000 | (uint(len(bits)) << 16) | ('E' << 8) | (0x20 + evKey))
	if err := ioctl(fd, req, uintptr(unsafe.Pointer(&bits[0]))); err != nil {
		return false
	}
	return bits[code/8]&(1<<(code%8)) != 0
}

type virtualKeyboard struct {
	fd int
}

func newVirtualKeyboard(name string, keys []uint16) (*virtualKeyboard, error) {
	fd, err := unix.Open("/dev/uinput", unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w (are you in the 'input' group, or is a udev rule / ACL needed?)", err)
	}
	// UI_SET_EVBIT / UI_SET_KEYBIT take the value itself as the ioctl arg.
	if err := ioctl(fd, uiSetEvBit, evKey); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("UI_SET_EVBIT: %w", err)
	}
	for _, k := range keys {
		if err := ioctl(fd, uiSetKeyBit, uintptr(k)); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("UI_SET_KEYBIT %d: %w", k, err)
		}
	}
	var setup uinputSetup
	setup.Bustype = 0x06 // BUS_VIRTUAL
	setup.Vendor = 0x34bf
	setup.Product = 0xff0b
	setup.Version = 1
	copy(setup.Name[:], name)
	if err := ioctl(fd, uiDevSetup, uintptr(unsafe.Pointer(&setup))); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("UI_DEV_SETUP: %w", err)
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}
	return &virtualKeyboard{fd: fd}, nil
}

func (v *virtualKeyboard) emit(code uint16, value int32) error {
	evs := [2]inputEvent{
		{Type: evKey, Code: code, Value: value},
		{Type: evSyn, Code: synReport, Value: 0},
	}
	buf := (*[2 * unsafe.Sizeof(inputEvent{})]byte)(unsafe.Pointer(&evs[0]))[:]
	_, err := unix.Write(v.fd, buf)
	return err
}

func (v *virtualKeyboard) close() {
	ioctl(v.fd, uiDevDestroy, 0)
	unix.Close(v.fd)
}

func main() {
	// Every flag can also be set through PADDLEKEYS_<NAME> environment
	// variables, which is how /etc/default/paddlekeys configures the service.
	var (
		devPath  = flag.String("device", envOr("DEVICE", ""), "evdev node of the paddle adapter (default: auto-detect)")
		match    = flag.String("match", envOr("MATCH", "CWUSB"), "substring of the input device name to auto-detect (empty = name not used)")
		usbID    = flag.String("usb-id", envOr("USB_ID", "34bf:ff0b"), "USB vendor:product id to auto-detect (empty = id not used)")
		leftKey  = flag.String("left", envOr("LEFT", "["), "key to send for the left paddle (BTN_LEFT)")
		rightKey = flag.String("right", envOr("RIGHT", "]"), "key to send for the right paddle (BTN_RIGHT)")
		midKey   = flag.String("middle", envOr("MIDDLE", ""), "key to send for BTN_MIDDLE, if the adapter has one (empty = ignore)")
		swap     = flag.Bool("swap", envBool("SWAP", false), "swap left and right")
		noGrab   = flag.Bool("no-grab", envBool("NO_GRAB", false), "don't grab the device exclusively (mouse clicks will still reach the desktop)")
		monitor  = flag.Bool("monitor", false, "just print every event from the device; don't create a virtual keyboard")
		verbose  = flag.Bool("v", envBool("VERBOSE", false), "log each paddle event")
		listKeys = flag.Bool("list-keys", false, "list known key names and exit")
		showVer  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("paddlekeys", version)
		return
	}

	var vid, pid uint16
	if *usbID != "" {
		var err error
		if vid, pid, err = parseUSBID(*usbID); err != nil {
			fatal(err)
		}
	}

	if *listKeys {
		names := make([]string, 0, len(keyNames))
		for n := range keyNames {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Println(strings.Join(names, " "))
		return
	}

	if *swap {
		*leftKey, *rightKey = *rightKey, *leftKey
	}
	mapping := map[uint16]uint16{}
	var keys []uint16
	for btn, name := range map[uint16]string{btnLeft: *leftKey, btnRight: *rightKey, btnMiddle: *midKey} {
		if name == "" {
			continue
		}
		code, err := lookupKey(name)
		if err != nil {
			fatal(err)
		}
		mapping[btn] = code
		keys = append(keys, code)
	}

	var vk *virtualKeyboard
	if !*monitor {
		var err error
		vk, err = newVirtualKeyboard("paddlekeys virtual keyboard", keys)
		if err != nil {
			fatal(err)
		}
		defer vk.close()
		fmt.Fprintf(os.Stderr, "paddlekeys %s: virtual keyboard created: left=%s right=%s", version, *leftKey, *rightKey)
		if *midKey != "" {
			fmt.Fprintf(os.Stderr, " middle=%s", *midKey)
		}
		fmt.Fprintln(os.Stderr)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	stop := make(chan struct{})
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "stopping")
		close(stop)
	}()

	// Outer loop handles unplug/replug: wait for the device, run until it
	// goes away, repeat.
	for {
		select {
		case <-stop:
			return
		default:
		}
		path := *devPath
		if path == "" {
			p, err := findDevice(*match, vid, pid)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v; waiting for device...\n", err)
				if !sleepOrStop(time.Second, stop) {
					return
				}
				continue
			}
			path = p
		}
		err := run(path, mapping, vk, !*noGrab, *monitor || *verbose, stop)
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if *devPath != "" && !errors.Is(err, unix.ENODEV) && !os.IsNotExist(err) {
			os.Exit(1)
		}
		if !sleepOrStop(time.Second, stop) {
			return
		}
	}
}

func sleepOrStop(d time.Duration, stop <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-stop:
		return false
	}
}

func run(path string, mapping map[uint16]uint16, vk *virtualKeyboard, grab, log bool, stop <-chan struct{}) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer unix.Close(fd)
	fmt.Fprintf(os.Stderr, "reading %s (%q)\n", path, deviceName(fd))

	if grab {
		if err := ioctl(fd, eviocGrab, 1); err != nil {
			return fmt.Errorf("EVIOCGRAB %s: %w", path, err)
		}
		defer ioctl(fd, eviocGrab, 0)
		fmt.Fprintln(os.Stderr, "device grabbed; its mouse clicks no longer reach the desktop")
	}

	// Release any keys we hold if we exit mid-press.
	held := map[uint16]bool{}
	defer func() {
		if vk == nil {
			return
		}
		for code, down := range held {
			if down {
				vk.emit(code, 0)
			}
		}
	}()

	// Reader goroutine so we can also react to stop.
	type result struct {
		ev  inputEvent
		err error
	}
	events := make(chan result)
	go func() {
		var ev inputEvent
		buf := (*[unsafe.Sizeof(inputEvent{})]byte)(unsafe.Pointer(&ev))[:]
		for {
			n, err := unix.Read(fd, buf)
			if err != nil {
				if err == unix.EINTR {
					continue
				}
				events <- result{err: fmt.Errorf("read %s: %w", path, err)}
				return
			}
			if n != len(buf) {
				events <- result{err: fmt.Errorf("short read %d from %s", n, path)}
				return
			}
			events <- result{ev: ev}
		}
	}()

	for {
		select {
		case <-stop:
			return nil
		case r := <-events:
			if r.err != nil {
				return r.err
			}
			ev := r.ev
			if ev.Type != evKey {
				if log && ev.Type != evSyn && ev.Type != evMsc {
					fmt.Fprintf(os.Stderr, "  other event type=%d code=%d value=%d\n", ev.Type, ev.Code, ev.Value)
				}
				continue
			}
			if ev.Value == 2 { // autorepeat; irrelevant for a paddle
				continue
			}
			code, ok := mapping[ev.Code]
			if log {
				name := buttonNames[ev.Code]
				if name == "" {
					name = fmt.Sprintf("code %d", ev.Code)
				}
				state := "up"
				if ev.Value == 1 {
					state = "DOWN"
				}
				if ok && vk != nil {
					fmt.Fprintf(os.Stderr, "%s %s -> key %d %s\n", name, state, code, state)
				} else {
					fmt.Fprintf(os.Stderr, "%s %s\n", name, state)
				}
			}
			if !ok || vk == nil {
				continue
			}
			if err := vk.emit(code, ev.Value); err != nil {
				return fmt.Errorf("uinput write: %w", err)
			}
			held[code] = ev.Value == 1
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
