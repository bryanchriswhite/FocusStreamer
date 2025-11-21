# X11 PutImage Investigation Report

## Problem Statement
Virtual display window remains black despite correct window capture and allowlist configuration. Investigation revealed fundamental issues with X11 PutImage implementation in the xgb Go library.

## System Configuration
- **OS**: Manjaro Linux (Kernel 6.16.8-1)
- **Display Server**: X11 (Xorg on :0)
- **Compositor**: KWin (X11 mode)
- **GPU**: AMD RX 580 (amdgpu driver)
- **Screen Depth**: 24 bits (TrueColor)
- **Root Visual**: ID 33
- **Display Resolution**: 4000x2560

## X11 Server Configuration

### Depth and Format
```
Depth: 24 planes
BitsPerPixel: 32 (4 bytes/pixel)
ScanlinePad: 32 bits
```

### Color Masks (from xdpyinfo)
```
Red mask:   0xff0000 (bits 16-23, byte 2)
Green mask: 0xff00   (bits 8-15, byte 1)
Blue mask:  0xff     (bits 0-7, byte 0)
```

**Memory layout**: `[Blue, Green, Red, X]` where X is padding byte

### PixmapFormats
```
Depth=1,  BitsPerPixel=1,  ScanlinePad=32
Depth=4,  BitsPerPixel=8,  ScanlinePad=32
Depth=8,  BitsPerPixel=8,  ScanlinePad=32
Depth=15, BitsPerPixel=16, ScanlinePad=32
Depth=16, BitsPerPixel=16, ScanlinePad=32
Depth=24, BitsPerPixel=32, ScanlinePad=32  ← Our depth
Depth=32, BitsPerPixel=32, ScanlinePad=32
```

## Investigation Timeline

### Attempt 1: Persistent Graphics Context
**Hypothesis**: Creating GC per-frame causes resource corruption

**Implementation**:
- Created single persistent GC in `Start()`
- Reused GC across all `putImage()` calls
- Added proper cleanup in `Stop()`

**Result**: ❌ FAILED
- Still getting BadLength errors
- Error format: `BadLength {Sequence: N, BadValue: <GC_ID>, MajorOpcode: 72}`

### Attempt 2: Scanline Padding
**Hypothesis**: Image data not properly padded to scanline boundaries

**Implementation**:
- Queried server's PixmapFormats to get scanlinePad value
- Calculated stride: `((width * bytesPerPixel + padBytes - 1) / padBytes) * padBytes`
- For 1920x1080x4: unpadded=7680, stride=7680 (already aligned)

**Result**: ❌ FAILED
- Padding was already correct (7680 divisible by 4)
- Still getting BadLength errors

**Key Discovery**: Depth 24 uses 32 bits per pixel, not 24!

### Attempt 3: Color Format Fix
**Hypothesis**: Wrong byte order or padding byte value

**Implementation**:
- Matched X11 visual masks: BGR byte order
- Set padding byte to 0 for depth 24 (not alpha channel)
- Format: `data[i] = B, data[i+1] = G, data[i+2] = R, data[i+3] = 0`

**Result**: ❌ FAILED
- Color format correct per X11 spec
- Still getting BadLength errors

### Attempt 4: Fresh GC Per Call
**Hypothesis**: GC has wrong attributes or is corrupted

**Implementation**:
- Created new GC with explicit foreground/background attributes
- Freed GC after each putImage call
- Tested with both window and pixmap as drawable

**Result**: ❌ FAILED
- Error persists with fresh GC
- Same error with pixmap as target
- Confirms issue is not GC-related

### Attempt 5: Pixmap Intermediate
**Hypothesis**: Can't draw directly to window, need pixmap

**Implementation**:
```go
pixmap = CreatePixmap(depth=24, drawable=window, width=1920, height=1080)
gc = CreateGC(drawable=pixmap)
PutImage(drawable=pixmap, gc=gc, ...)
CopyArea(src=pixmap, dst=window, gc=gc, ...)
```

**Result**: ❌ FAILED
- PutImage fails on pixmap too
- Error: `BadLength {Sequence: 6, BadValue: 104857603, MajorOpcode: 72}`

## Root Cause Analysis

### Verified Correct Parameters
All X11 protocol parameters verified as correct:
- ✅ Image dimensions: 1920x1080
- ✅ Color depth: 24
- ✅ Bits per pixel: 32
- ✅ Scanline padding: 7680 bytes (aligned to 32-bit boundary)
- ✅ Data size: 8,294,400 bytes (1920×1080×4)
- ✅ Color format: BGRx matching visual masks
- ✅ Padding byte: 0 (not alpha)
- ✅ Graphics context: Valid and created successfully
- ✅ Drawable: Valid window/pixmap

### Error Pattern
```
BadLength {NiceName: Length, Sequence: N, BadValue: <Resource_ID>, MinorOpcode: 0, MajorOpcode: 72}
```

- **MajorOpcode 72**: X11 PutImage request
- **BadValue**: Always contains GC ID (not window ID)
- **BadLength**: X11 error indicating request length mismatch
- **Pattern**: Error occurs on first PutImage call, subsequent calls get BadRequest

### Critical Discovery
The GC ID in BadValue field reveals the true issue:
```
Window ID: 104857601
GC ID:     104857602  ← This appears in error's BadValue field
```

X11 CreateGC succeeds without error, but server rejects the GC when used in PutImage. This suggests the xgb library is encoding the PutImage request incorrectly.

## Conclusion

**The xgb library's PutImage implementation has a bug or incompatibility** with our X11 server configuration. Despite all parameters being correct per X11 protocol specification, the request is malformed at the wire protocol level.

### Evidence
1. All X11 parameters verified correct via independent tools (xdpyinfo, xwininfo)
2. Error occurs with multiple GC creation methods
3. Error occurs with both windows and pixmaps
4. GC creation succeeds but GC is rejected in PutImage
5. Other xgb operations work fine (CreateWindow, CreateGC, MapWindow, etc.)

## Performance Considerations for Real-Time Streaming

### Current Bottlenecks
1. **PutImage Unusable**: Cannot use xgb's PutImage at all
2. **XComposite Capture Works**: Successfully capturing windows via XComposite
3. **PNG Encoding**: Current pipeline encodes to PNG for transfer (inefficient)

### Target Requirements
- **Frame Rate**: 30+ FPS
- **Resolution**: 1440p+ (2560x1440 or higher)
- **Latency**: < 100ms end-to-end

### Data Volume Analysis
At 1440p (2560x1440), 30 FPS, BGRA format:
```
Per frame: 2560 × 1440 × 4 = 14,745,600 bytes (~14 MB)
Per second: 14.7 MB × 30 = 441 MB/s
```

This is too much for uncompressed transfer over typical networks.

## Recommended Solutions

### Option A: CGo + Xlib (Most Reliable)
Use CGo to call Xlib's `XPutImage` directly, bypassing xgb.

**Pros**:
- Xlib is the reference implementation
- Guaranteed to work
- Mature and well-tested
- Can use Xlib's optimized functions

**Cons**:
- Requires CGo (complicates builds)
- Less "pure Go"
- Slightly more complex FFI

**Performance**: Excellent for PutImage, but still limited by X11 protocol overhead

### Option B: Shared Memory (XShm) - Best for Performance
Use MIT-SHM extension to share memory between client and X server.

**Pros**:
- Zero-copy rendering (no data sent over X11 socket)
- Fastest possible X11 rendering method
- Native Go xgb support exists
- Ideal for high FPS scenarios

**Cons**:
- Requires shared memory support
- More complex setup
- Only works locally (not over network)

**Performance**: Can easily achieve 60+ FPS at 1440p+

### Option C: Skip X11 Rendering Entirely (Recommended for Streaming)
Since the goal is Discord screen sharing, we don't need a visible window.

**Architecture**:
```
XComposite Capture → Direct Encode → Network Stream
         ↓
    (No X11 window rendering)
```

**Implementation Options**:

1. **Virtual V4L2 Device** (loopback camera)
   - Create `/dev/video` device
   - Discord sees it as webcam
   - Use `v4l2loopback` kernel module

2. **GStreamer Pipeline**
   - Hardware-accelerated encoding (VA-API/NVENC)
   - RTP/WebRTC streaming
   - Can target Discord directly

3. **FFmpeg Integration**
   - Pipe frames to ffmpeg
   - Hardware encoding
   - Multiple output formats

**Pros**:
- Eliminates X11 rendering bottleneck entirely
- Hardware-accelerated encoding
- Lower CPU usage
- Better quality/bitrate control
- Can achieve 60+ FPS at 4K

**Cons**:
- No preview window (unless we add separate lightweight preview)
- More complex architecture
- Depends on external tools (v4l2loopback, gstreamer, etc.)

### Option D: OpenGL/Vulkan Rendering
Use GPU for compositing and rendering.

**Pros**:
- GPU-accelerated
- Can handle effects/transforms
- Very high performance

**Cons**:
- Complex implementation
- Still limited by X11 protocol for final display
- Overkill for simple mirroring

## Recommendation

**For the stated goal of 30fps+ 1440p+ real-time video for Discord:**

**Use Option C (Skip X11 Rendering) with V4L2 Virtual Camera**

### Rationale
1. Discord already supports virtual cameras
2. Eliminates X11 rendering bottleneck
3. Hardware-accelerated encoding possible
4. Can easily hit 60 FPS at 4K with modern GPUs
5. Lower CPU usage than software rendering
6. Better quality control via encoder settings

### Architecture
```
┌─────────────────────┐
│  Window Manager     │
│  (XComposite        │
│   Capture)          │
└──────────┬──────────┘
           │ Raw BGRA frames
           ↓
┌─────────────────────┐
│  Frame Processor    │
│  - Scaling          │
│  - Color conversion │
└──────────┬──────────┘
           │ YUV420/NV12
           ↓
┌─────────────────────┐
│  Encoder            │
│  - VA-API (AMD/Intel)│
│  - NVENC (NVIDIA)   │
└──────────┬──────────┘
           │ H264/H265
           ↓
┌─────────────────────┐
│  V4L2 Loopback      │
│  /dev/videoN        │
└─────────────────────┘
           │
           ↓
     Discord picks as camera
```

### Implementation Path
1. Keep current XComposite capture (working)
2. Add v4l2loopback integration
3. Add hardware encoder (VA-API for AMD)
4. Stream encoded frames to virtual camera
5. Discord selects virtual camera in settings

### Alternative: If Preview Window Required
Use **Option B (XShm)** for preview window + **Option C** for streaming:
- Small preview window via XShm (low overhead)
- Main stream goes directly to v4l2loopback
- Best of both worlds

## Next Steps

1. **Decision Point**: Choose between:
   - Pure streaming (no preview) - Option C
   - Preview + streaming - Option B + C hybrid
   - Fix PutImage (Option A) - not recommended for performance

2. **If choosing streaming path**:
   - Research v4l2loopback Go bindings
   - Investigate VA-API Go bindings for AMD hardware encoding
   - Design frame pipeline architecture
   - Benchmark capture → encode → stream latency

3. **If choosing XShm path**:
   - Implement XShm in display manager
   - Test performance at target resolution/FPS
   - Measure CPU/memory overhead

## References

- X11 Protocol: https://www.x.org/releases/X11R7.7/doc/xproto/x11protocol.html
- XShm Extension: https://www.x.org/releases/X11R7.7/doc/xextproto/shm.html
- v4l2loopback: https://github.com/umlaeute/v4l2loopback
- VA-API: https://github.com/intel/libva
- xgb library: https://github.com/BurntSushi/xgb

## Appendix: Test Code

See `/home/bwhite/Projects/FocusStreamer/test_putimage.go` and `test_pixmap.go` for reproduction tests.
