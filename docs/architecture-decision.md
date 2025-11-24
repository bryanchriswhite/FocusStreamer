# Architecture Decision: Production Implementation

## Executive Summary

After systematic investigation and analysis of production applications (Chrome, Discord, OBS), we recommend implementing **FFmpeg + V4L2 Loopback virtual camera** approach.

## Key Findings

### What We've Built ✅
- **XComposite window capture**: Working perfectly
- **Allowlist filtering**: Working
- **Window focus tracking**: Working
- **Web UI**: Working

### What's Blocking ⚠️
- **X11 PutImage**: xgb library bug prevents window rendering
- All parameters verified correct per X11 spec
- Not fixable without patching xgb or using Xlib via CGo

### Performance Requirements 🎯
- **Target**: 30+ FPS @ 1440p+
- **Current bottleneck**: Cannot render to display window
- **New approach needed**: Skip X11 rendering, go direct to Discord

## How Production Apps Work

### Chrome/Chromium
```
XComposite → WebRTC Encoder (FFmpeg/libavcodec) → Network
```
- Uses VA-API/NVENC hardware encoding
- Same XComposite capture we use

### Discord
```
Chromium's APIs → Hardware encoder → Streaming
```
- Electron app using Chromium's screen capture
- Expects either: screen capture OR camera device (V4L2)

### OBS Studio Virtual Camera
```
XComposite → FFmpeg encoder → V4L2 loopback → Apps see as camera
```
- Gold standard for Linux streaming
- Exactly what we should do

## Three Options Analyzed

| Aspect | FFmpeg+V4L2 | GStreamer | PipeWire |
|--------|-------------|-----------|----------|
| **Industry Use** | OBS, Streamers | Multimedia apps | Modern desktop |
| **Maturity** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Implementation** | 4-6 hours | 8-12 hours | 16-24 hours |
| **Hardware Encoding** | ✅ VA-API | ✅ VA-API | ✅ Possible |
| **Dependencies** | FFmpeg, kernel module | GStreamer libs | PipeWire daemon |
| **Go Integration** | Easy (exec) | Medium (bindings) | Hard (CGo) |
| **Documentation** | Extensive | Good | Limited |
| **Discord Support** | ✅ V4L2 cameras | ✅ V4L2 cameras | ⚠️ Experimental |

## Recommended Architecture

### Phase 1: MVP (4-6 hours)
```
┌────────────────────┐
│ XComposite Capture │  ← Already working
│ (Window Manager)   │
└─────────┬──────────┘
          │ Raw BGRA frames (30 FPS)
          ↓
┌────────────────────┐
│  Frame Pipeline    │  ← NEW
│  - Rate limiting   │
│  - Format check    │
└─────────┬──────────┘
          │ Pipe (stdin)
          ↓
┌────────────────────┐
│  FFmpeg Process    │  ← NEW
│  - h264_vaapi      │  (AMD GPU encoding)
│  - Quality: 5Mbps  │
└─────────┬──────────┘
          │ H.264 stream
          ↓
┌────────────────────┐
│ /dev/video20       │  ← v4l2loopback
│ (Virtual Camera)   │
└─────────┬──────────┘
          │
          ↓
     Discord sees
   "FocusStreamer" camera
```

### Phase 2: Optimization (Later)
- Add XDamage for damage tracking (50-80% CPU reduction)
- Implement frame buffering
- Add quality presets
- Monitor encoding performance

### Phase 3: Advanced (Optional)
- Migrate to PipeWire when Go bindings mature
- Direct WebRTC streaming
- Multi-output support

## Implementation Details

### FFmpeg Command
```bash
ffmpeg -f rawvideo \
  -pixel_format bgra \
  -video_size 1920x1080 \
  -framerate 30 \
  -i - \
  -c:v h264_vaapi \
  -vaapi_device /dev/dri/renderD128 \
  -vf 'format=nv12,hwupload' \
  -b:v 5M -maxrate 7M -bufsize 10M \
  -g 60 -keyint_min 60 \
  -f v4l2 /dev/video20
```

### Go Integration
```go
type Encoder struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    width  int
    height int
    fps    int
}

func (e *Encoder) Start() error {
    e.cmd = exec.Command("ffmpeg",
        "-f", "rawvideo",
        "-pixel_format", "bgra",
        "-video_size", fmt.Sprintf("%dx%d", e.width, e.height),
        "-framerate", fmt.Sprintf("%d", e.fps),
        "-i", "-",
        "-c:v", "h264_vaapi",
        "-vaapi_device", "/dev/dri/renderD128",
        "-vf", "format=nv12,hwupload",
        "-f", "v4l2", "/dev/video20",
    )

    stdin, err := e.cmd.StdinPipe()
    if err != nil {
        return err
    }
    e.stdin = stdin

    return e.cmd.Start()
}

func (e *Encoder) WriteFrame(frame []byte) error {
    _, err := e.stdin.Write(frame)
    return err
}
```

### Setup Steps
```bash
# 1. Install v4l2loopback
sudo pacman -S v4l2loopback-dkms

# 2. Load module (or add to /etc/modules-load.d/)
sudo modprobe v4l2loopback \
  devices=1 \
  video_nr=20 \
  card_label="FocusStreamer" \
  exclusive_caps=1

# 3. Verify device exists
ls -la /dev/video20

# 4. Test in Discord
# Settings → Voice & Video → Camera → Select "FocusStreamer"
```

## Performance Expectations

### Bandwidth Comparison
- **Raw (current)**: 1920×1080×4 bytes × 30 FPS = **441 MB/s** ❌
- **H.264 @ 5 Mbps**: **5 MB/s** ✅ (88x reduction)

### CPU Usage (estimated)
- **XComposite capture**: ~2-5% (already doing this)
- **VA-API encoding**: ~5-10% (GPU does heavy lifting)
- **Total overhead**: ~7-15% CPU

### Achievable Performance
- **1080p @ 60 FPS**: ✅ Easy
- **1440p @ 60 FPS**: ✅ Doable
- **4K @ 30 FPS**: ✅ Possible
- **4K @ 60 FPS**: ⚠️ Depends on GPU

### Latency
- **Encoding**: ~10-30ms (1-2 frames)
- **V4L2**: ~5-10ms
- **Total added**: ~15-40ms (acceptable for Discord)

## Why This Beats Fixing PutImage

### Fixing PutImage (Don't do this)
- ❌ Still have to encode for Discord anyway
- ❌ 441 MB/s uncompressed data
- ❌ No hardware acceleration
- ❌ Requires patching xgb or CGo Xlib
- ❌ Still need virtual camera for Discord
- ⏱️ 8-16 hours to fix + implement encoding

### FFmpeg + V4L2 (Do this)
- ✅ Skip X11 rendering entirely
- ✅ Hardware encoding included
- ✅ 5-10 MB/s compressed
- ✅ Industry standard approach
- ✅ Works with Discord immediately
- ⏱️ 4-6 hours to implement

**Time saved: 4-10 hours**
**Performance gained: 10-100x**

## Migration from Current Code

### What Stays
- ✅ Window Manager (XComposite capture)
- ✅ Config Manager
- ✅ Web UI
- ✅ API Server
- ✅ Allowlist logic

### What Goes
- ❌ Display Manager (internal/display/manager.go)
  - Remove all PutImage code
  - Remove GC management
  - Remove window rendering

### What's New
- ➕ Encoder package (internal/encoder/)
  - FFmpeg process management
  - Frame pipeline
  - Quality presets
  - Health monitoring

## Risks & Mitigations

### Risk: FFmpeg not installed
**Mitigation**: Check in startup, provide clear error with install instructions

### Risk: v4l2loopback module not available
**Mitigation**: Detect and provide setup instructions, systemd service to load module

### Risk: VA-API not working
**Mitigation**: Fallback to software encoding (libx264), warn user about CPU usage

### Risk: Discord doesn't see camera
**Mitigation**: Document setup process, check permissions (/dev/video* access)

## Success Metrics

### MVP Success
- [ ] Virtual camera appears in Discord
- [ ] 1080p @ 30 FPS stable
- [ ] < 15% CPU usage
- [ ] < 100ms latency
- [ ] Allowlist filtering works

### Production Success
- [ ] 1440p @ 60 FPS stable
- [ ] < 20% CPU usage
- [ ] Handles window resize/movement
- [ ] Survives FFmpeg crashes (restart)
- [ ] Configurable quality settings

## Next Steps

1. **Prototype** (1-2 hours)
   - Load v4l2loopback manually
   - Test FFmpeg command with test pattern
   - Verify Discord sees camera

2. **Integration** (2-3 hours)
   - Create encoder package
   - Pipe XComposite frames to FFmpeg
   - Handle errors and restarts

3. **Polish** (1-2 hours)
   - Add quality presets
   - Configuration options
   - Startup checks
   - Documentation

4. **Testing** (1 hour)
   - Test various window sizes
   - Test frame rates
   - Measure CPU/latency
   - Test in actual Discord call

## Alternative: Quick Prototype First

Before full implementation, we can test the concept:

```bash
# Terminal 1: Load module and run FocusStreamer
sudo modprobe v4l2loopback devices=1 video_nr=20 card_label="FocusStreamer"
./focusstreamer serve

# Terminal 2: Pipe screenshots to FFmpeg
while true; do
  import -window $(xdotool getwindowfocus) -depth 24 bgra:- | \
  ffmpeg -f rawvideo -pixel_format bgra -video_size 1920x1080 -framerate 10 -i - \
    -c:v h264_vaapi -vaapi_device /dev/dri/renderD128 \
    -vf 'format=nv12,hwupload' -f v4l2 /dev/video20
done
```

This proves the concept before writing code.

## Conclusion

**The path forward is clear**: Implement FFmpeg + V4L2 loopback virtual camera.

This is:
- ✅ How OBS does it (battle-tested)
- ✅ What Discord expects (V4L2 camera)
- ✅ Fast to implement (4-6 hours)
- ✅ High performance (hardware encoding)
- ✅ Future-proof (can migrate to PipeWire later)

**Estimated time to working Discord integration: 1 day**

vs fixing PutImage → still needing encoding → same result but 2-3 days
