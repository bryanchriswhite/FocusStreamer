#!/bin/bash
# Continuous stream for testing Discord integration
# Press Ctrl+C to stop

set -e

WIDTH=1920
HEIGHT=1080
FPS=30
DEVICE="/dev/video20"

echo "=== FocusStreamer Continuous Test Stream ==="
echo
echo "Streaming animated test pattern to $DEVICE"
echo "This will run until you press Ctrl+C"
echo
echo "While this runs:"
echo "1. Open Discord"
echo "2. Go to Settings → Voice & Video → Camera"
echo "3. Select 'FocusStreamer' from the camera dropdown"
echo "4. You should see an animated test pattern"
echo
echo "Press Ctrl+C to stop streaming..."
echo

# Stream continuously until interrupted
# Note: v4l2loopback expects raw video, not encoded
# Discord will encode it when streaming
ffmpeg -re \
    -f lavfi \
    -i testsrc=size=${WIDTH}x${HEIGHT}:rate=${FPS} \
    -pix_fmt yuv420p \
    -f v4l2 "$DEVICE"
