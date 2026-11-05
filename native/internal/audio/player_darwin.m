//go:build darwin && cgo && !portableaudio

#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#include <math.h>

typedef struct { double position; double duration; int ready; int failed; int ended; int playing; } hm_audio_state;

@interface HMAudio : NSObject
@property(nonatomic, strong) AVPlayer *player;
@property(nonatomic, strong) id observer;
@property(nonatomic, strong) dispatch_queue_t queue;
@property(nonatomic) BOOL ended;
@property(nonatomic,strong) id activity;
@property(nonatomic,strong) id loader;
@end
@implementation HMAudio
@end

void *hm_audio_from_asset(AVURLAsset *asset,id loader) {
    @autoreleasepool {
        HMAudio *owner = [HMAudio new];
        owner.queue = dispatch_queue_create("org.tinglan.audio", DISPATCH_QUEUE_SERIAL);
        owner.loader=loader;
        dispatch_sync(owner.queue, ^{
            owner.player = [AVPlayer playerWithPlayerItem:[AVPlayerItem playerItemWithAsset:asset]];
            owner.player.volume = 0;
            __weak HMAudio *weakOwner = owner;
            owner.observer = [[NSNotificationCenter defaultCenter]
                addObserverForName:AVPlayerItemDidPlayToEndTimeNotification
                object:owner.player.currentItem queue:nil usingBlock:^(NSNotification *note) {
                    HMAudio *strongOwner = weakOwner;
                    if (strongOwner) dispatch_async(strongOwner.queue, ^{ strongOwner.ended = YES; });
                }];
        });
        return (__bridge_retained void *)owner;
    }
}
void *hm_audio_open(const char *path) {
    NSURL *url=[NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
    return hm_audio_from_asset([AVURLAsset URLAssetWithURL:url options:@{AVURLAssetPreferPreciseDurationAndTimingKey:@YES}],nil);
}
void hm_audio_play(void *raw) {
    HMAudio *owner = (__bridge HMAudio *)raw;
    dispatch_sync(owner.queue, ^{
        if (!owner.activity) owner.activity = [[NSProcessInfo processInfo] beginActivityWithOptions:NSActivityUserInitiatedAllowingIdleSystemSleep reason:@"Music playback"];
        [owner.player play];
    });
}
void hm_audio_pause(void *raw) {
    HMAudio *owner = (__bridge HMAudio *)raw;
    dispatch_sync(owner.queue, ^{ [owner.player pause];
        if (owner.activity) { [[NSProcessInfo processInfo] endActivity:owner.activity]; owner.activity=nil; }
    });
}
void hm_audio_seek(void *raw, double seconds) {
    HMAudio *owner = (__bridge HMAudio *)raw;
    dispatch_sync(owner.queue, ^{
        owner.ended = NO;
        double duration = CMTimeGetSeconds(owner.player.currentItem.duration);
        double target = isfinite(duration) && duration > 0 ? fmin(seconds,duration) : seconds;
        [owner.player seekToTime:CMTimeMakeWithSeconds(target, 1000000)
            toleranceBefore:kCMTimeZero toleranceAfter:kCMTimeZero];
    });
}
void hm_audio_volume(void *raw, float volume) {
    HMAudio *owner = (__bridge HMAudio *)raw;
    dispatch_sync(owner.queue, ^{ owner.player.volume = volume; });
}
hm_audio_state hm_audio_snapshot(void *raw) {
    HMAudio *owner = (__bridge HMAudio *)raw;
    __block hm_audio_state result = {0};
    dispatch_sync(owner.queue, ^{
        AVPlayerItem *item = owner.player.currentItem;
        double position = CMTimeGetSeconds(owner.player.currentTime);
        double duration = CMTimeGetSeconds(item.duration);
        result.position = isfinite(position) ? fmax(0,position) : 0;
        result.duration = isfinite(duration) ? duration : 0;
        result.ready = item.status == AVPlayerItemStatusReadyToPlay;
        result.failed = item.status == AVPlayerItemStatusFailed;
        result.playing = owner.player.timeControlStatus == AVPlayerTimeControlStatusPlaying;
        result.ended = owner.ended;
        if (result.ended && owner.activity) { [[NSProcessInfo processInfo] endActivity:owner.activity]; owner.activity=nil; }
    });
    return result;
}
void hm_audio_close(void *raw) {
    HMAudio *owner = (__bridge_transfer HMAudio *)raw;
    dispatch_sync(owner.queue, ^{
        [[NSNotificationCenter defaultCenter] removeObserver:owner.observer];
        owner.observer = nil;
        if (owner.activity) { [[NSProcessInfo processInfo] endActivity:owner.activity]; owner.activity=nil; }
        [owner.player pause];
        [owner.player replaceCurrentItemWithPlayerItem:nil];
        owner.player = nil;
    });
    if ([owner.loader respondsToSelector:@selector(close)]) [owner.loader close];
    owner.loader=nil;
}
