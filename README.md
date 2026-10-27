# paddlekeys

Makes the cheap **"HAM CWUSB"** morse paddle USB adapter (USB ID `34bf:ff0b`)
work with [VBand](https://hamradio.solutions/vband/) on Linux.

The adapter presents itself as a USB mouse: the left paddle is a left click
and the right paddle is a right click. VBand wants keyboard keys instead:
`[` for dits and `]` for dahs. paddlekeys sits in between, hides the mouse
clicks from the desktop, and turns each paddle press into a key press.

- Starts automatically when the adapter is plugged in, stops when it is removed.
- Works on Wayland and X11 (the keys are injected at the kernel level).
- Single static binary, no dependencies. Debian/Ubuntu, Fedora and Arch packages.
- Any key can be mapped; defaults are what VBand expects.

## Install

### Debian / Ubuntu / Mint / Raspberry Pi OS

Download the `.deb` for your CPU from the
[latest release](https://github.com/silverark/paddlekeys/releases/latest)
(`amd64` for a normal PC, `arm64` for a 64-bit Raspberry Pi, `armv7` for 32-bit) and:

```
sudo apt install ./paddlekeys_*_linux_amd64.deb
```

### Fedora / openSUSE

```
sudo dnf install ./paddlekeys_*_linux_amd64.rpm
```

### Arch

```
sudo pacman -U ./paddlekeys_*_linux_amd64.pkg.tar.zst
```

### What the package sets up

- `/usr/bin/paddlekeys`, the program.
- A udev rule that starts the `paddlekeys` service when the adapter is
  plugged in and stops it when unplugged, and makes `/dev/uinput` writable
  by the `input` group.
- `/etc/default/paddlekeys`, the configuration file.

Plug the adapter in and it just works. Nothing runs when it isn't connected.

### Just the binary

If you'd rather not install a package, download `paddlekeys_linux_<arch>`
from the release page and run it from a terminal:

```
chmod +x paddlekeys_linux_amd64
./paddlekeys_linux_amd64 -v
```

Leave it running while you use VBand. You need read access to
`/dev/input/event*` and write access to `/dev/uinput`. On most distros that
means being in the `input` group and adding one udev rule; see
[Permissions](#permissions).

### From source

Needs Go 1.22 or newer.

```
git clone https://github.com/silverark/paddlekeys
cd paddlekeys
make build        # produces ./paddlekeys
./paddlekeys -v
```

## Using it with VBand

1. Plug in the adapter.
2. Open [VBand](https://hamradio.solutions/vband/). In Settings, leave
   **"Use Mouse Buttons" off**. Pick your keyer mode (Iambic A/B, straight key…).
3. Click somewhere on the VBand page so it has keyboard focus, and key away.

If dits and dahs are the wrong way round, either tick **Flip Paddle** in
VBand or set `PADDLEKEYS_SWAP=yes` in the config file (see below).

The VBand tab must be focused, exactly as with any keyboard-based adapter.
If you key while another window is focused you'll type a row of `[` and
`]` characters into it. Holding a paddle produces a stream of repeated
characters; that's normal keyboard auto-repeat and VBand ignores it.

## Configuration

Edit `/etc/default/paddlekeys`, then `sudo systemctl restart paddlekeys`.
Every setting is commented out by default, which gives the VBand defaults.

| Setting | Default | Meaning |
|---|---|---|
| `PADDLEKEYS_LEFT` | `[` | Key sent for the left paddle |
| `PADDLEKEYS_RIGHT` | `]` | Key sent for the right paddle |
| `PADDLEKEYS_SWAP` | `no` | Swap left and right |
| `PADDLEKEYS_MIDDLE` | (none) | Key for a middle button, if your adapter has one |
| `PADDLEKEYS_VERBOSE` | `no` | Log every paddle press to the journal |
| `PADDLEKEYS_USB_ID` | `34bf:ff0b` | USB vendor:product to look for |
| `PADDLEKEYS_MATCH` | `CWUSB` | Substring of the input device name to look for |
| `PADDLEKEYS_DEVICE` | (auto) | Explicit `/dev/input/eventN` to use instead of searching |

Key names: `[`, `]`, `leftctrl`, `rightctrl`, letters, digits, `space`,
`f1`…`f12` and more. `paddlekeys -list-keys` prints the full list. A bare
number is taken as a raw Linux key code. VBand also accepts
`leftctrl`/`rightctrl` in place of `[`/`]`.

The same settings work as command-line flags when running by hand:

```
paddlekeys -swap
paddlekeys -left leftctrl -right rightctrl
paddlekeys -v                 # log each paddle event
paddlekeys -monitor           # print what the adapter sends, change nothing
paddlekeys -no-grab           # don't hide the mouse clicks from the desktop
paddlekeys -version
```

## Checking it's working

```
systemctl status paddlekeys          # should be "active (running)" while plugged in
journalctl -u paddlekeys -f          # live log; add PADDLEKEYS_VERBOSE=yes to see presses
```

A healthy start looks like:

```
paddlekeys 1.0.0: virtual keyboard created: left=[ right=]
reading /dev/input/event8 ("HAM HAM CWUSB")
device grabbed; its mouse clicks no longer reach the desktop
```

## Troubleshooting

**Service isn't running after plugging in.** Check the adapter is seen:
`lsusb | grep 34bf`. If it is, `sudo udevadm trigger` re-runs the rule.
If the ID is different, your adapter is a different model; set
`PADDLEKEYS_USB_ID` and the udev rule in `/usr/lib/udev/rules.d/80-paddlekeys.rules`
to match.

**"EVIOCGRAB … device or resource busy".** Something else already has the
adapter grabbed, usually a second copy of paddlekeys. Stop the service
before running it by hand: `sudo systemctl stop paddlekeys`.

**"open /dev/uinput: permission denied"** when running by hand. See
[Permissions](#permissions).

**Right-click menus pop up when keying.** paddlekeys isn't running or was
started with `-no-grab`.

**Nothing happens in VBand but the log shows presses.** The VBand tab isn't
focused, or "Use Mouse Buttons" is on in its settings.

## Permissions

The packaged service runs as its own unprivileged user and needs nothing
from you. Running the binary by hand needs:

- read access to `/dev/input/event*`: `sudo usermod -aG input $USER`, then log out and in;
- write access to `/dev/uinput`, which is root-only on most distros. Add
  this rule (the package installs it for you):

  ```
  # /etc/udev/rules.d/80-uinput.rules
  KERNEL=="uinput", GROUP="input", MODE="0660", OPTIONS+="static_node=uinput"
  ```

  then `sudo udevadm control --reload && sudo udevadm trigger`.

## Windows and macOS

paddlekeys is Linux-only: it relies on the Linux kernel's evdev and uinput
interfaces. The same adapter can be made to work elsewhere with existing
per-device remapping tools, with no code required.

**macOS: [Karabiner-Elements](https://karabiner-elements.pqrs.org/).**
It can remap pointing-device buttons to keys. Add a Complex Modification
with two `from` entries, `pointing_button: button1` → `key_code: open_bracket`
and `pointing_button: button2` → `key_code: close_bracket`, and a
`device_if` condition with `vendor_id: 13503` and `product_id: 65291`
(0x34bf and 0xff0b in decimal) so your real mouse is unaffected. Karabiner
needs Input Monitoring permission in System Settings.

**Windows: [AutoHotInterception](https://github.com/evilC/AutoHotInterception).**
Windows can't normally tell which mouse a click came from. AutoHotInterception
uses the Interception driver to subscribe to a specific device by vendor and
product ID (`0x34BF`, `0xFF0B`) and block its clicks while sending
`[` and `]` with `Send`. Install Interception, then AutoHotkey, then follow
the AutoHotInterception README's "subscribe mouse button" example.

In both cases leave VBand's "Use Mouse Buttons" option off.

## Development

```
make build                   # static binary with the version from VERSION
go vet ./...
./paddlekeys -monitor -v     # watch raw events without changing anything
```

Project layout:

- `main.go`: the whole program. Talks to evdev and uinput directly via
  ioctls; the only dependency is `golang.org/x/sys`.
- `packaging/`: systemd unit, udev rules, default config and the
  install/remove scripts that go into the packages.
- `.goreleaser.yaml`: builds amd64/arm64/armv7 binaries, tarballs, `.deb`,
  `.rpm` and Arch packages.
- `.github/workflows/ci.yml`: vet, build, package snapshot and a
  smoke-install of the `.deb` on every push.
- `.github/workflows/release.yml`: publishes a GitHub release on `v*` tags.

### Releasing

The version lives in the `VERSION` file. `clog release` merges the
changelog fragments and tags the commit `v<VERSION>`. Pushing that tag
triggers the release workflow, which checks the tag matches `VERSION`,
builds everything and attaches it to a GitHub release.

```
# edit VERSION, add a changelog fragment in changelog.d/
clog release
git push --follow-tags
```
