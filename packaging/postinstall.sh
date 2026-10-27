#!/bin/sh
set -e
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload-rules || true
    # Re-evaluate already plugged in devices so the service starts now.
    udevadm trigger --subsystem-match=usb --attr-match=idVendor=34bf --attr-match=idProduct=ff0b || true
    udevadm trigger --subsystem-match=misc --attr-match=name=uinput || true
fi
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload || true
    # Make sure a running instance (upgrade) picks up the new binary.
    if systemctl is-active --quiet paddlekeys.service; then
        systemctl restart paddlekeys.service || true
    fi
fi
exit 0
