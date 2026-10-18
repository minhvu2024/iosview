/*
 H264Streamer — hardware H.264 screen stream for iOSView (added to TrollVNC).

 Captured frames (the same IOSurface-backed frames the VNC path uses) are
 copied with the IOSurface accelerator (GPU/scaler, no CPU) and encoded by
 VideoToolbox's hardware encoder in real-time mode (no B-frames, one frame
 in flight). Access units are pushed to every client of a plain TCP port.

 Wire format (server -> client), one message per encoded frame:
   u32 BE  payload length N
   u16 BE  width   (encoded, portrait)
   u16 BE  height
   u8      flags   (bit0 = keyframe)
   u8      rotation quadrant to apply when drawing (0..3, 90° clockwise each)
   u16     reserved
   N bytes H.264 Annex-B (keyframes start with SPS + PPS)

 Client -> server:
   'K'                 request a keyframe
   'B' + u32 BE kbps   set target bitrate (100..20000)

 Everything here is best effort: if VideoToolbox is unavailable the stream
 port simply sends nothing and the VNC server keeps working.
*/

#ifndef H264Streamer_h
#define H264Streamer_h

#import <CoreMedia/CoreMedia.h>
#import <Foundation/Foundation.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef int (*TVH264RotationFn)(void);

/// Configure before TVH264Start. bitrateKbps/maxFps <= 0 use defaults.
void TVH264Configure(int port, int bitrateKbps, int maxFps, TVH264RotationFn rotationFn,
                     void (^clientsChanged)(int clientCount));

/// Start listening (no-op if port <= 0). Returns NO if the port can't be bound.
BOOL TVH264Start(void);

/// Number of connected stream clients.
int TVH264ClientCount(void);

/// Feed a captured frame (main thread). Cheap no-op when there are no clients.
void TVH264PushFrame(CMSampleBufferRef sampleBuffer);

#ifdef __cplusplus
}
#endif

#endif /* H264Streamer_h */
