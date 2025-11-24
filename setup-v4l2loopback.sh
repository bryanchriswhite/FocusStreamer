#!/bin/bash
# FocusStreamer V4L2 Loopback Setup Script
# Run this script with sudo to load the v4l2loopback kernel module

set -e

echo "=== FocusStreamer V4L2 Loopback Setup ==="
echo

# Kill any processes using /dev/video20 first
if [ -e /dev/video20 ]; then
    echo "Checking for processes using /dev/video20..."
    if fuser /dev/video20 2>/dev/null; then
        echo "Killing processes using /dev/video20..."
        fuser -k /dev/video20 2>/dev/null || true
        sleep 1
    fi
fi

# Unload module if already loaded (to reset configuration)
if lsmod | grep -q v4l2loopback; then
    echo "Unloading existing v4l2loopback module..."
    modprobe -r v4l2loopback
    sleep 1
fi

echo "Loading v4l2loopback module..."
modprobe v4l2loopback \
    devices=1 \
    video_nr=20 \
    card_label="FocusStreamer" \
    exclusive_caps=0 \
    max_buffers=2

echo "✓ Module loaded successfully"

echo
echo "Verifying /dev/video20 device:"
if [ -e /dev/video20 ]; then
    ls -la /dev/video20
    echo
    echo "✓ Virtual camera device created successfully"
    echo
    echo "Device information:"
    v4l2-ctl --device=/dev/video20 --all 2>&1 | head -20
else
    echo "✗ ERROR: /dev/video20 not found"
    exit 1
fi

echo
echo "=== Setup Complete ==="
echo "Discord should now be able to see 'FocusStreamer' as a camera device"
echo
echo "To make this persistent across reboots, add to /etc/modules-load.d/v4l2loopback.conf:"
echo "v4l2loopback"
echo
echo "And create /etc/modprobe.d/v4l2loopback.conf with:"
echo "options v4l2loopback devices=1 video_nr=20 card_label=\"FocusStreamer\" exclusive_caps=0 max_buffers=2"
