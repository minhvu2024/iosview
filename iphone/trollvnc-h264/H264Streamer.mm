/*
 H264Streamer — hardware H.264 screen stream for iOSView. See H264Streamer.h.
*/

#if !__has_feature(objc_arc)
#warning This file must be compiled with ARC. Use -fobjc-arc flag.
#endif

#import <Accelerate/Accelerate.h>
#import <CoreVideo/CoreVideo.h>
#import <QuartzCore/QuartzCore.h>
#import <VideoToolbox/VideoToolbox.h>

#import <arpa/inet.h>
#import <dlfcn.h>
#import <errno.h>
#import <fcntl.h>
#import <netinet/in.h>
#import <netinet/tcp.h>
#import <sys/socket.h>
#import <unistd.h>

#import <atomic>

#import "H264Streamer.h"
#import "IOKitSPI.h"
#import "IOSurfaceSPI.h"
#import "Logging.h"

// A client that is this many frames behind gets its backlog dropped and
// waits for the next keyframe (keeps latency bounded on slow Wi-Fi).
static const int kMaxQueuedPerClient = 3;
// Encoder backlog limit: newest frame wins, older ones are skipped.
static const int kMaxInFlight = 2;
static const int kPoolThreshold = 6;

#pragma mark - Client

@interface TVH264Client : NSObject {
  @public
    int fd;
    dispatch_queue_t sendQ;
    dispatch_source_t readSrc;
    std::atomic<int> pending;
    BOOL needKey;
    BOOL closed;
    uint8_t inbuf[8];
    int inlen;
}
@end

@implementation TVH264Client
@end

#pragma mark - State

static int gPort = 0;
static int gBitrateKbps = 2500;
static int gMaxFps = 60;     // current stream frame rate (main thread)
static int gDefaultFps = 60; // configured rate, restored when no client is left
static int gScalePct = 100;  // encoded size in % of the screen (main thread)
static TVH264RotationFn gRotFn = NULL;
static void (^gClientsChanged)(int) = nil;
static void (^gFpsChanged)(int) = nil;

// Network state: owned by gNetQ.
static dispatch_queue_t gNetQ;
static dispatch_source_t gListenSrc;
static NSMutableArray<TVH264Client *> *gClients;
static std::atomic<int> gClientCount(0);

// Requests from the network side to the encoder (main thread).
static std::atomic<bool> gWantKey(false);
static std::atomic<int> gNewBitrateKbps(0);

// Encoder state: main thread only (the VT callback only touches atomics).
static VTCompressionSessionRef gSession = NULL;
static int gEncW = 0, gEncH = 0;
static CVPixelBufferPoolRef gPool = NULL;
static IOSurfaceAcceleratorRef gAccel = NULL;
static BOOL gAccelTried = NO;
static CVPixelBufferRef gPendingPB = NULL; // newest captured frame not yet encoded
static CVPixelBufferRef gLastPB = NULL;    // last frame sent to the encoder
static BOOL gPumpScheduled = NO;
static CFTimeInterval gLastEncodeAt = 0;
static CFTimeInterval gSessionFailedAt = -100;
static uint64_t gGeneration = 0;
static int gRefinesLeft = 0;
static std::atomic<int> gInFlight(0);

static void pumpEncoder(void);

#pragma mark - Small helpers

static void put16(uint8_t *p, uint32_t v) {
    p[0] = (uint8_t)(v >> 8);
    p[1] = (uint8_t)v;
}

static void put32(uint8_t *p, uint32_t v) {
    p[0] = (uint8_t)(v >> 24);
    p[1] = (uint8_t)(v >> 16);
    p[2] = (uint8_t)(v >> 8);
    p[3] = (uint8_t)v;
}

static BOOL writeAll(int fd, const uint8_t *p, size_t n) {
    while (n > 0) {
        ssize_t w = send(fd, p, n, 0);
        if (w < 0) {
            if (errno == EINTR)
                continue;
            return NO;
        }
        p += w;
        n -= (size_t)w;
    }
    return YES;
}

static void setPB(CVPixelBufferRef *slot, CVPixelBufferRef pb) {
    if (*slot == pb)
        return;
    if (pb)
        CVPixelBufferRetain(pb);
    if (*slot)
        CVPixelBufferRelease(*slot);
    *slot = pb;
}

#pragma mark - Encoder (main thread)

static void teardownEncoder(void) {
    if (gSession) {
        // Drain pending output callbacks first so none run after gInFlight is reset.
        VTCompressionSessionCompleteFrames(gSession, kCMTimeInvalid);
        VTCompressionSessionInvalidate(gSession);
        CFRelease(gSession);
        gSession = NULL;
    }
    if (gPool) {
        CVPixelBufferPoolRelease(gPool);
        gPool = NULL;
    }
    setPB(&gPendingPB, NULL);
    setPB(&gLastPB, NULL);
    gEncW = gEncH = 0;
    gInFlight.store(0);
}

static void applyBitrate(int kbps) {
    if (!gSession)
        return;
    if (kbps < 100)
        kbps = 100;
    if (kbps > 20000)
        kbps = 20000;
    gBitrateKbps = kbps;
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_AverageBitRate, (__bridge CFTypeRef) @(kbps * 1000));
    // Allow short bursts (scrolling) but cap one second at 1.5x the target.
    NSArray *limits = @[ @(kbps * 1000 / 8 * 3 / 2), @1 ];
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_DataRateLimits, (__bridge CFArrayRef)limits);
}

static void vtOutputImpl(void *sourceRefCon, OSStatus status, VTEncodeInfoFlags infoFlags, CMSampleBufferRef sb);

static void vtOutput(void *refCon, void *sourceRefCon, OSStatus status, VTEncodeInfoFlags infoFlags,
                     CMSampleBufferRef sb) {
    // VT's callback thread is not guaranteed to drain an autorelease pool per frame.
    @autoreleasepool {
        vtOutputImpl(sourceRefCon, status, infoFlags, sb);
    }
}

static void vtOutputImpl(void *sourceRefCon, OSStatus status, VTEncodeInfoFlags infoFlags, CMSampleBufferRef sb) {
    gInFlight.fetch_sub(1);
    if (status != noErr || !sb || (infoFlags & kVTEncodeInfo_FrameDropped) || !CMSampleBufferDataIsReady(sb)) {
        if ((intptr_t)sourceRefCon & 4) {
            // A forced keyframe was lost: waiting clients (needKey) would stall on a
            // static screen. Re-request it and re-encode the last frame.
            gWantKey.store(true);
            dispatch_async(dispatch_get_main_queue(), ^{
                if (gSession && gLastPB && !gPendingPB) {
                    setPB(&gPendingPB, gLastPB);
                    pumpEncoder();
                }
            });
        }
        return;
    }

    BOOL key = YES;
    CFArrayRef atts = CMSampleBufferGetSampleAttachmentsArray(sb, false);
    if (atts && CFArrayGetCount(atts) > 0) {
        CFDictionaryRef d = (CFDictionaryRef)CFArrayGetValueAtIndex(atts, 0);
        CFBooleanRef notSync = NULL;
        if (CFDictionaryGetValueIfPresent(d, kCMSampleAttachmentKey_NotSync, (const void **)&notSync) && notSync)
            key = !CFBooleanGetValue(notSync);
    }

    CMFormatDescriptionRef fmt = CMSampleBufferGetFormatDescription(sb);
    if (!fmt)
        return;
    CMVideoDimensions dim = CMVideoFormatDescriptionGetDimensions(fmt);
    size_t psCount = 0;
    int nalLen = 4;
    CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, 0, NULL, NULL, &psCount, &nalLen);
    if (nalLen < 1 || nalLen > 4)
        nalLen = 4;

    static const uint8_t sc[4] = {0, 0, 0, 1};
    NSMutableData *pkt = [NSMutableData dataWithLength:12];

    if (key) {
        for (size_t i = 0; i < psCount; i++) {
            const uint8_t *ps = NULL;
            size_t psLen = 0;
            if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, i, &ps, &psLen, NULL, NULL) == noErr && ps) {
                [pkt appendBytes:sc length:4];
                [pkt appendBytes:ps length:psLen];
            }
        }
    }

    // AVCC (length-prefixed NAL units) -> Annex-B (start codes).
    CMBlockBufferRef bb = CMSampleBufferGetDataBuffer(sb);
    size_t total = bb ? CMBlockBufferGetDataLength(bb) : 0;
    if (total == 0)
        return;
    NSMutableData *raw = [NSMutableData dataWithLength:total];
    if (CMBlockBufferCopyDataBytes(bb, 0, total, raw.mutableBytes) != kCMBlockBufferNoErr)
        return;
    const uint8_t *p = (const uint8_t *)raw.bytes;
    size_t off = 0;
    while (off + (size_t)nalLen <= total) {
        uint32_t n = 0;
        for (int j = 0; j < nalLen; j++)
            n = (n << 8) | p[off + j];
        off += (size_t)nalLen;
        if (n == 0 || n > total - off)
            break;
        [pkt appendBytes:sc length:4];
        [pkt appendBytes:p + off length:n];
        off += n;
    }

    uint8_t *h = (uint8_t *)pkt.mutableBytes;
    put32(h, (uint32_t)(pkt.length - 12));
    put16(h + 4, (uint32_t)dim.width);
    put16(h + 6, (uint32_t)dim.height);
    h[8] = key ? 1 : 0;
    h[9] = (uint8_t)((intptr_t)sourceRefCon & 3);
    h[10] = h[11] = 0;

    NSData *frame = pkt;
    dispatch_async(gNetQ, ^{
        for (TVH264Client *c in [gClients copy]) {
            if (c->closed)
                continue;
            if (c->needKey && !key)
                continue;
            if (c->pending.load() >= kMaxQueuedPerClient) {
                // Too far behind: drop its backlog, resync on a keyframe.
                c->needKey = YES;
                gWantKey.store(true);
                continue;
            }
            if (key)
                c->needKey = NO;
            c->pending.fetch_add(1);
            dispatch_async(c->sendQ, ^{
                BOOL ok = writeAll(c->fd, (const uint8_t *)frame.bytes, frame.length);
                c->pending.fetch_sub(1);
                if (!ok) {
                    dispatch_async(gNetQ, ^{
                        if (c->readSrc)
                            dispatch_source_cancel(c->readSrc);
                    });
                }
            });
        }
    });

    if (key)
        TVLogVerbose(@"H264: keyframe %dx%d %lu bytes", dim.width, dim.height, (unsigned long)frame.length);
}

static BOOL createSession(int w, int h, BOOL lowLatency) {
    NSDictionary *spec = nil;
    if (lowLatency) {
        if (@available(iOS 14.5, *)) {
            spec = @{(__bridge NSString *)kVTVideoEncoderSpecification_EnableLowLatencyRateControl : @YES};
        } else {
            return NO;
        }
    }
    NSDictionary *srcAttrs = @{
        (__bridge NSString *)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_32BGRA),
        (__bridge NSString *)kCVPixelBufferWidthKey : @(w),
        (__bridge NSString *)kCVPixelBufferHeightKey : @(h),
        (__bridge NSString *)kCVPixelBufferIOSurfacePropertiesKey : @{},
    };
    VTCompressionSessionRef s = NULL;
    OSStatus st =
        VTCompressionSessionCreate(kCFAllocatorDefault, w, h, kCMVideoCodecType_H264, (__bridge CFDictionaryRef)spec,
                                   (__bridge CFDictionaryRef)srcAttrs, kCFAllocatorDefault, vtOutput, NULL, &s);
    if (st != noErr || !s) {
        TVLog(@"H264: VTCompressionSessionCreate(lowLatency=%d) failed: %d", (int)lowLatency, (int)st);
        return NO;
    }
    gSession = s;
    TVLog(@"H264: encoder ready %dx%d lowLatency=%d bitrate=%dkbps fps<=%d", w, h, (int)lowLatency, gBitrateKbps,
          gMaxFps);
    return YES;
}

static BOOL ensureSession(int w, int h) {
    if (gSession && gEncW == w && gEncH == h)
        return YES;
    CFTimeInterval now = CACurrentMediaTime();
    if (!gSession && now - gSessionFailedAt < 5.0)
        return NO;
    teardownEncoder();

    if (!createSession(w, h, YES) && !createSession(w, h, NO)) {
        gSessionFailedAt = now;
        return NO;
    }

    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_H264EntropyMode, kVTH264EntropyMode_CABAC);
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse); // no B-frames
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_MaxFrameDelayCount, (__bridge CFTypeRef) @0);
    // Keyframes only on demand (new viewer / resync); TCP never loses data.
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_MaxKeyFrameInterval, (__bridge CFTypeRef) @7200);
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_MaxKeyFrameIntervalDuration, (__bridge CFTypeRef) @120);
    VTSessionSetProperty(gSession, kVTCompressionPropertyKey_ExpectedFrameRate, (__bridge CFTypeRef) @(gMaxFps));
    applyBitrate(gBitrateKbps);
    VTCompressionSessionPrepareToEncodeFrames(gSession);

    NSDictionary *poolAttrs = @{
        (__bridge NSString *)kCVPixelBufferPixelFormatTypeKey : @(kCVPixelFormatType_32BGRA),
        (__bridge NSString *)kCVPixelBufferWidthKey : @(w),
        (__bridge NSString *)kCVPixelBufferHeightKey : @(h),
        (__bridge NSString *)kCVPixelBufferIOSurfacePropertiesKey : @{},
    };
    if (CVPixelBufferPoolCreate(kCFAllocatorDefault, NULL, (__bridge CFDictionaryRef)poolAttrs, &gPool) !=
        kCVReturnSuccess) {
        TVLog(@"H264: pixel buffer pool creation failed");
        teardownEncoder();
        gSessionFailedAt = now;
        return NO;
    }
    gEncW = w;
    gEncH = h;
    gWantKey.store(true);
    return YES;
}

// Copy the capture surface into a pooled buffer (the capture surface is
// overwritten on the next display tick while the encoder may still read).
static CVPixelBufferRef copyFrame(CVPixelBufferRef src) {
    CVPixelBufferRef dst = NULL;
    NSDictionary *aux = @{(__bridge NSString *)kCVPixelBufferPoolAllocationThresholdKey : @(kPoolThreshold)};
    if (CVPixelBufferPoolCreatePixelBufferWithAuxAttributes(kCFAllocatorDefault, gPool, (__bridge CFDictionaryRef)aux,
                                                            &dst) != kCVReturnSuccess ||
        !dst)
        return NULL;

    if (!gAccelTried) {
        gAccelTried = YES;
        if (IOSurfaceAcceleratorCreate(kCFAllocatorDefault, NULL, &gAccel) != kIOReturnSuccess)
            gAccel = NULL;
    }
    IOSurfaceRef s = CVPixelBufferGetIOSurface(src);
    IOSurfaceRef d = CVPixelBufferGetIOSurface(dst);
    if (gAccel && s && d && IOSurfaceAcceleratorTransferSurface(gAccel, s, d, NULL, NULL, NULL, NULL) == kIOReturnSuccess)
        return dst;

    // CPU fallback (only if the accelerator is unavailable).
    CVPixelBufferLockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
    CVPixelBufferLockBaseAddress(dst, 0);
    const uint8_t *sp = (const uint8_t *)CVPixelBufferGetBaseAddress(src);
    uint8_t *dp = (uint8_t *)CVPixelBufferGetBaseAddress(dst);
    size_t sbpr = CVPixelBufferGetBytesPerRow(src), dbpr = CVPixelBufferGetBytesPerRow(dst);
    size_t sw = CVPixelBufferGetWidth(src), sh = CVPixelBufferGetHeight(src);
    size_t dw = CVPixelBufferGetWidth(dst), dh = CVPixelBufferGetHeight(dst);
    if (sp && dp && (sw != dw || sh != dh)) {
        vImage_Buffer s = {.data = (void *)sp, .height = sh, .width = sw, .rowBytes = sbpr};
        vImage_Buffer d = {.data = dp, .height = dh, .width = dw, .rowBytes = dbpr};
        vImageScale_ARGB8888(&s, &d, NULL, kvImageNoFlags);
    } else if (sp && dp) {
        size_t rows = MIN(sh, dh);
        size_t bytes = MIN(sbpr, dbpr);
        for (size_t y = 0; y < rows; y++)
            memcpy(dp + y * dbpr, sp + y * sbpr, bytes);
    }
    CVPixelBufferUnlockBaseAddress(dst, 0);
    CVPixelBufferUnlockBaseAddress(src, kCVPixelBufferLock_ReadOnly);
    return dst;
}

static void scheduleRefine(uint64_t gen, double delay) {
    // After motion stops, re-encode the final frame so the encoder can sharpen
    // it (a fast scroll ends on a soft frame otherwise).
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(delay * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        if (gen != gGeneration || gRefinesLeft <= 0 || !gLastPB || gPendingPB || gClientCount.load() == 0)
            return;
        gRefinesLeft--;
        setPB(&gPendingPB, gLastPB);
        pumpEncoder();
    });
}

static void encodeNow(void) {
    CVPixelBufferRef pb = gPendingPB;
    gPendingPB = NULL; // ownership moves to this function
    BOOL isRefine = (pb == gLastPB);
    bool key = gWantKey.exchange(false);

    int nb = gNewBitrateKbps.exchange(0);
    if (nb > 0)
        applyBitrate(nb);

    NSDictionary *props = key ? @{(__bridge NSString *)kVTEncodeFrameOptionKey_ForceKeyFrame : @YES} : nil;
    int rot = gRotFn ? (gRotFn() & 3) : 0;
    CMTime pts = CMTimeMakeWithSeconds(CACurrentMediaTime(), 1000000);
    gInFlight.fetch_add(1);
    OSStatus st = VTCompressionSessionEncodeFrame(gSession, pb, pts, kCMTimeInvalid, (__bridge CFDictionaryRef)props,
                                                  (void *)(intptr_t)(rot | (key ? 4 : 0)), NULL);
    gLastEncodeAt = CACurrentMediaTime();
    if (st != noErr) {
        gInFlight.fetch_sub(1);
        if (key)
            gWantKey.store(true);
        static int sErrLogs = 0;
        if (sErrLogs++ < 5)
            TVLog(@"H264: encode failed: %d", (int)st);
        if (st == kVTInvalidSessionErr) {
            CVPixelBufferRelease(pb);
            teardownEncoder(); // recreated on the next frame
            return;
        }
    }
    setPB(&gLastPB, pb);
    CVPixelBufferRelease(pb);

    gGeneration++;
    if (!isRefine)
        gRefinesLeft = 2;
    scheduleRefine(gGeneration, isRefine ? 0.4 : 0.15);
}

static void pumpEncoder(void) {
    if (!gPendingPB || !gSession)
        return;
    double minInterval = 1.0 / (double)(gMaxFps > 0 ? gMaxFps : 60) * 0.9;
    double wait = minInterval - (CACurrentMediaTime() - gLastEncodeAt);
    if (gInFlight.load() >= kMaxInFlight || wait > 0) {
        if (!gPumpScheduled) {
            gPumpScheduled = YES;
            double d = wait > 0.004 ? wait : 0.004;
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(d * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
                gPumpScheduled = NO;
                pumpEncoder();
            });
        }
        return;
    }
    encodeNow();
}

void TVH264PushFrame(CMSampleBufferRef sampleBuffer) {
    if (gClientCount.load() == 0)
        return;
    CVPixelBufferRef src = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (!src)
        return;
    int w = (int)CVPixelBufferGetWidth(src), h = (int)CVPixelBufferGetHeight(src);
    if (w <= 0 || h <= 0)
        return;
    if (gScalePct < 100) {
        // Smaller encode: the copy below scales on the GPU for free, the
        // encoder then has only a fraction of the pixels to compress.
        w = MAX(64, w * gScalePct / 100) & ~1;
        h = MAX(64, h * gScalePct / 100) & ~1;
    }
    if (!ensureSession(w, h))
        return;
    CVPixelBufferRef copy = copyFrame(src);
    if (!copy)
        return; // encoder backlog: this frame is skipped, a newer one follows
    setPB(&gPendingPB, copy);
    CVPixelBufferRelease(copy);
    pumpEncoder();
}

// Main thread. fps <= 0 / pct <= 0 = configured defaults. A new size takes
// effect on the next captured frame (ensureSession rebuilds the encoder and
// sends a keyframe); fpsChanged also forces that frame on a static screen.
static void applyProfile(int fps, int pct) {
    if (fps <= 0)
        fps = gDefaultFps;
    if (pct <= 0)
        pct = 100;
    if (fps == gMaxFps && pct == gScalePct)
        return;
    gMaxFps = fps;
    gScalePct = pct;
    if (gSession)
        VTSessionSetProperty(gSession, kVTCompressionPropertyKey_ExpectedFrameRate, (__bridge CFTypeRef) @(fps));
    TVLog(@"H264: profile fps=%d size=%d%%", fps, pct);
    if (gFpsChanged)
        gFpsChanged(fps == gDefaultFps ? 0 : fps);
}

#pragma mark - Network (gNetQ)

static void notifyClients(void) {
    int n = gClientCount.load();
    void (^cb)(int) = gClientsChanged;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (n == 0) {
            teardownEncoder();
            applyProfile(0, 0); // next viewer starts from the defaults
        } else if (gLastPB && !gPendingPB) {
            // Static screen: resend the last frame as a keyframe right away.
            setPB(&gPendingPB, gLastPB);
            pumpEncoder();
        }
        if (cb)
            cb(n);
    });
}

// The phone's UDID (what usbmuxd reports as SerialNumber), so iOSView can tell
// that this Wi-Fi address and a USB cable are the same phone.
static NSString *deviceUDID(void) {
    static NSString *udid = @"";
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        void *h = dlopen("/usr/lib/libMobileGestalt.dylib", RTLD_LAZY);
        typedef CFPropertyListRef (*MGCopyAnswerFn)(CFStringRef);
        MGCopyAnswerFn fn = h ? (MGCopyAnswerFn)dlsym(h, "MGCopyAnswer") : NULL;
        if (fn) {
            id v = CFBridgingRelease(fn(CFSTR("UniqueDeviceID")));
            if ([v isKindOfClass:[NSString class]])
                udid = v;
        }
    });
    return udid;
}

// Identity reply: a normal 12-byte frame header with flag bit 1 set, then the UDID text.
static void sendIdent(TVH264Client *c) {
    NSData *id8 = [deviceUDID() dataUsingEncoding:NSUTF8StringEncoding];
    NSMutableData *pkt = [NSMutableData dataWithLength:12];
    [pkt appendData:id8];
    uint8_t *h = (uint8_t *)pkt.mutableBytes;
    put32(h, (uint32_t)id8.length);
    h[8] = 2;
    dispatch_async(c->sendQ, ^{
        writeAll(c->fd, (const uint8_t *)pkt.bytes, pkt.length);
    });
}

// Length of a command with arguments (0 = not one).
static int commandLength(uint8_t c) {
    return c == 'B' ? 5 : c == 'P' ? 3 : 0;
}

static void handleClientBytes(TVH264Client *c, const uint8_t *buf, ssize_t n) {
    for (ssize_t i = 0; i < n; i++) {
        if (c->inlen == 0) {
            if (buf[i] == 'K') {
                gWantKey.store(true);
                continue;
            }
            if (buf[i] == 'I') {
                sendIdent(c);
                continue;
            }
            if (!commandLength(buf[i]))
                continue; // unknown command byte: ignore
        }
        c->inbuf[c->inlen++] = buf[i];
        if (c->inlen < commandLength(c->inbuf[0]))
            continue;
        c->inlen = 0;
        if (c->inbuf[0] == 'B') {
            uint32_t kbps = ((uint32_t)c->inbuf[1] << 24) | ((uint32_t)c->inbuf[2] << 16) |
                            ((uint32_t)c->inbuf[3] << 8) | c->inbuf[4];
            if (kbps >= 100 && kbps <= 20000)
                gNewBitrateKbps.store((int)kbps);
        } else { // 'P'
            int fps = c->inbuf[1] & 0x7f, pct = c->inbuf[2] & 0x7f;
            if (fps < 5 || fps > 60 || pct < 25 || pct > 100)
                continue;
            dispatch_async(dispatch_get_main_queue(), ^{
                applyProfile(fps, pct);
            });
        }
    }
}

static void acceptClients(int lfd) {
    for (;;) {
        int fd = accept(lfd, NULL, NULL);
        if (fd < 0)
            return;
        int fl = fcntl(fd, F_GETFL, 0);
        if (fl >= 0)
            fcntl(fd, F_SETFL, fl & ~O_NONBLOCK);
        int one = 1;
        setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, sizeof(one));
        // Detect viewers that vanished while the screen is static (nothing sent).
        setsockopt(fd, SOL_SOCKET, SO_KEEPALIVE, &one, sizeof(one));
        int idle = 5, intvl = 2, cnt = 3;
        setsockopt(fd, IPPROTO_TCP, TCP_KEEPALIVE, &idle, sizeof(idle));
        setsockopt(fd, IPPROTO_TCP, TCP_KEEPINTVL, &intvl, sizeof(intvl));
        setsockopt(fd, IPPROTO_TCP, TCP_KEEPCNT, &cnt, sizeof(cnt));
        // Small kernel send buffer: on congested Wi-Fi, frames back up in our
        // per-client queue (where they are dropped -> keyframe resync) instead
        // of piling up seconds of stale video inside the socket.
        int sndbuf = 64 * 1024;
        setsockopt(fd, SOL_SOCKET, SO_SNDBUF, &sndbuf, sizeof(sndbuf));
#ifdef TCP_NOTSENT_LOWAT
        int lowat = 16 * 1024;
        setsockopt(fd, IPPROTO_TCP, TCP_NOTSENT_LOWAT, &lowat, sizeof(lowat));
#endif
        struct timeval tv = {3, 0};
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));

        TVH264Client *c = [TVH264Client new];
        c->fd = fd;
        c->sendQ = dispatch_queue_create("trollvnc.h264.send", DISPATCH_QUEUE_SERIAL);
        c->needKey = YES;
        c->pending.store(0);

        dispatch_source_t rs = dispatch_source_create(DISPATCH_SOURCE_TYPE_READ, (uintptr_t)fd, 0, gNetQ);
        c->readSrc = rs;
        __weak TVH264Client *wc = c;
        dispatch_source_set_event_handler(rs, ^{
            TVH264Client *cc = wc;
            if (!cc)
                return;
            uint8_t buf[64];
            ssize_t r = recv(cc->fd, buf, sizeof(buf), MSG_DONTWAIT);
            if (r > 0) {
                handleClientBytes(cc, buf, r);
            } else if (r == 0 || (errno != EAGAIN && errno != EINTR)) {
                dispatch_source_cancel(cc->readSrc);
            }
        });
        dispatch_source_set_cancel_handler(rs, ^{
            // Runs on gNetQ once; close the socket after queued sends finish.
            c->closed = YES;
            [gClients removeObject:c];
            gClientCount.store((int)gClients.count);
            TVLog(@"H264: client gone, clients=%d", gClientCount.load());
            shutdown(c->fd, SHUT_RDWR);
            dispatch_async(c->sendQ, ^{
                close(c->fd);
            });
            notifyClients();
        });
        [gClients addObject:c];
        gClientCount.store((int)gClients.count);
        gWantKey.store(true);
        dispatch_resume(rs);
        TVLog(@"H264: client connected, clients=%d", gClientCount.load());
        notifyClients();
    }
}

#pragma mark - Public

void TVH264Configure(int port, int bitrateKbps, int maxFps, TVH264RotationFn rotationFn,
                     void (^clientsChanged)(int clientCount), void (^fpsChanged)(int fps)) {
    gPort = port;
    if (bitrateKbps > 0)
        gBitrateKbps = MAX(100, MIN(20000, bitrateKbps));
    if (maxFps > 0)
        gMaxFps = MAX(5, MIN(120, maxFps));
    gDefaultFps = gMaxFps;
    gRotFn = rotationFn;
    gClientsChanged = [clientsChanged copy];
    gFpsChanged = [fpsChanged copy];
}

int TVH264ClientCount(void) {
    return gClientCount.load();
}

BOOL TVH264Start(void) {
    if (gPort <= 0 || gListenSrc)
        return gListenSrc != nil;
    gNetQ = dispatch_queue_create("trollvnc.h264.net", DISPATCH_QUEUE_SERIAL);
    gClients = [NSMutableArray array];

    int lfd = socket(AF_INET, SOCK_STREAM, 0);
    if (lfd < 0)
        return NO;
    int one = 1;
    setsockopt(lfd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_len = sizeof(addr);
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)gPort);
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    if (bind(lfd, (struct sockaddr *)&addr, sizeof(addr)) != 0 || listen(lfd, 8) != 0) {
        TVLog(@"H264: cannot listen on port %d: %s", gPort, strerror(errno));
        close(lfd);
        return NO;
    }
    fcntl(lfd, F_SETFL, fcntl(lfd, F_GETFL, 0) | O_NONBLOCK);
    gListenSrc = dispatch_source_create(DISPATCH_SOURCE_TYPE_READ, (uintptr_t)lfd, 0, gNetQ);
    dispatch_source_set_event_handler(gListenSrc, ^{
        acceptClients(lfd);
    });
    dispatch_resume(gListenSrc);
    TVLog(@"H264: stream listening on port %d (bitrate %d kbps, fps<=%d)", gPort, gBitrateKbps, gMaxFps);
    return YES;
}
