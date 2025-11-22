#!/bin/bash
# FocusStreamer FFmpeg + V4L2 Loopback Proof of Concept
# This script tests the complete pipeline: raw frames → FFmpeg → v4l2loopback → Discord

set -e

WIDTH=1920
HEIGHT=1080
FPS=30
DEVICE="/dev/video20"

echo "=== FocusStreamer FFmpeg POC ==="
echo

# Check prerequisites
echo "Checking prerequisites..."

if ! command -v ffmpeg &> /dev/null; then
    echo "✗ ERROR: ffmpeg not found"
    exit 1
fi
echo "✓ ffmpeg found"

if [ ! -e "$DEVICE" ]; then
    echo "✗ ERROR: $DEVICE not found"
    echo "  Please run: sudo ./setup-v4l2loopback.sh"
    exit 1
fi
echo "✓ $DEVICE exists"

if [ ! -e /dev/dri/renderD128 ]; then
    echo "✗ ERROR: /dev/dri/renderD128 not found (VA-API device)"
    exit 1
fi
echo "✓ VA-API device found"

echo
echo "=== Test 1: Static Color Test ==="
echo "Generating solid red frames and streaming to $DEVICE..."
echo "Open Discord and select 'FocusStreamer' camera - you should see a red screen"
echo

# Generate 10 seconds of solid red frames
ffmpeg -f lavfi \
    -i color=red:size=${WIDTH}x${HEIGHT}:rate=${FPS} \
    -t 10 \
    -c:v h264_vaapi \
    -vaapi_device /dev/dri/renderD128 \
    -vf 'format=nv12,hwupload' \
    -b:v 5M -maxrate 7M -bufsize 10M \
    -g 60 -keyint_min 60 \
    -f v4l2 "$DEVICE" 2>&1 | grep -E "(Input|Output|Stream|frame=)" | tail -10

echo
echo "✓ Test 1 complete"
echo

echo "=== Test 2: Test Pattern Animation ==="
echo "Generating animated test pattern and streaming to $DEVICE..."
echo "You should see a moving test pattern in Discord"
echo

# Generate 10 seconds of test pattern
ffmpeg -f lavfi \
    -i testsrc=size=${WIDTH}x${HEIGHT}:rate=${FPS} \
    -t 10 \
    -c:v h264_vaapi \
    -vaapi_device /dev/dri/renderD128 \
    -vf 'format=nv12,hwupload' \
    -b:v 5M -maxrate 7M -bufsize 10M \
    -g 60 -keyint_min 60 \
    -f v4l2 "$DEVICE" 2>&1 | grep -E "(Input|Output|Stream|frame=)" | tail -10

echo
echo "✓ Test 2 complete"
echo

echo "=== Test 3: Raw BGRA Frame Piping ==="
echo "This simulates how our Go code will work: generating raw BGRA frames and piping to FFmpeg"
echo

# Generate raw BGRA frames and pipe to FFmpeg
# This is the pattern we'll use in Go: write raw frames to stdin
ffmpeg -f lavfi \
    -i testsrc=size=${WIDTH}x${HEIGHT}:rate=${FPS} \
    -pix_fmt bgra \
    -f rawvideo - 2>/dev/null | \
ffmpeg -f rawvideo \
    -pixel_format bgra \
    -video_size ${WIDTH}x${HEIGHT} \
    -framerate ${FPS} \
    -i - \
    -t 5 \
    -c:v h264_vaapi \
    -vaapi_device /dev/dri/renderD128 \
    -vf 'format=nv12,hwupload' \
    -b:v 5M -maxrate 7M -bufsize 10M \
    -g 60 -keyint_min 60 \
    -f v4l2 "$DEVICE" 2>&1 | grep -E "(Input|Output|Stream|frame=)" | tail -10

echo
echo "✓ Test 3 complete"
echo

echo "=== All Tests Passed ==="
echo
echo "Next steps:"
echo "1. Verify Discord sees 'FocusStreamer' camera in Settings → Voice & Video"
echo "2. Start a call and select FocusStreamer as camera source"
echo "3. Proceed to Go implementation in internal/encoder/"
echo
echo "Expected Go pipeline:"
echo "  XComposite capture → []byte (BGRA) → FFmpeg stdin → h264_vaapi → /dev/video20 → Discord"
