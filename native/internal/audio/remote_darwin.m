//go:build darwin && cgo && !portableaudio

#import <AVFoundation/AVFoundation.h>
#include <stdint.h>
#include <stdlib.h>

extern int hm_audio_read(uintptr_t, unsigned long long, long long, int, void **);
extern void hm_audio_cancel_read(uintptr_t, unsigned long long);
extern void *hm_audio_from_asset(AVURLAsset *asset, id loader);

@interface HMResourceLoader : NSObject <AVAssetResourceLoaderDelegate>
@property(nonatomic) uintptr_t handle;
@property(nonatomic) long long total;
@property(nonatomic,copy) NSString *type;
@property(nonatomic,strong) dispatch_queue_t control;
@property(nonatomic,strong) dispatch_group_t workers;
@property(nonatomic,strong) NSMutableSet<AVAssetResourceLoadingRequest *> *pending;
@property(nonatomic) BOOL closed;
- (void)close;
@end

@implementation HMResourceLoader
- (BOOL)resourceLoader:(AVAssetResourceLoader *)loader shouldWaitForLoadingOfRequestedResource:(AVAssetResourceLoadingRequest *)request {
    if (self.closed) return NO;
    request.contentInformationRequest.contentType = self.type;
    request.contentInformationRequest.contentLength = self.total;
    request.contentInformationRequest.byteRangeAccessSupported = YES;
    if (!request.dataRequest) { [request finishLoading]; return YES; }
    [self.pending addObject:request];
    unsigned long long token = (unsigned long long)(__bridge void *)request;
    long long start = request.dataRequest.currentOffset;
    if (start == 0) start = request.dataRequest.requestedOffset;
    long long end = request.dataRequest.requestsAllDataToEndOfResource ? self.total : MIN(self.total, request.dataRequest.requestedOffset + request.dataRequest.requestedLength);
    dispatch_group_async(self.workers, dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        long long offset = start;
        while (offset < end) {
            // A request can span an entire lossless track. Drain each chunk's
            // autoreleased NSData/HTTP objects instead of retaining them until
            // the dispatch worker finishes the full request.
            @autoreleasepool {
            __block BOOL active;
            dispatch_sync(self.control, ^{ active = !self.closed && [self.pending containsObject:request]; });
            if (!active) break;
            int length = (int)MIN(1024*1024, end-offset);
            void *bytes = NULL;
            int count = hm_audio_read(self.handle, token, offset, length, &bytes);
            NSData *data = count > 0 ? [NSData dataWithBytesNoCopy:bytes length:count freeWhenDone:YES] : nil;
            dispatch_sync(self.control, ^{
                if (self.closed || ![self.pending containsObject:request]) return;
                if (count <= 0) {
                    [request finishLoadingWithError:[NSError errorWithDomain:@"HMMedia" code:1 userInfo:nil]];
                    [self.pending removeObject:request];
                } else { [request.dataRequest respondWithData:data]; }
            });
            if (count <= 0) break;
            offset += count;
            }
        }
        dispatch_sync(self.control, ^{
            if ([self.pending containsObject:request]) {
                [self.pending removeObject:request];
                if (!self.closed) [request finishLoading];
            }
        });
    });
    return YES;
}
- (void)resourceLoader:(AVAssetResourceLoader *)loader didCancelLoadingRequest:(AVAssetResourceLoadingRequest *)request {
    [self.pending removeObject:request];
    hm_audio_cancel_read(self.handle, (unsigned long long)(__bridge void *)request);
}
- (void)close {
    dispatch_sync(self.control, ^{
        self.closed = YES;
        for (AVAssetResourceLoadingRequest *request in self.pending) {
            hm_audio_cancel_read(self.handle, (unsigned long long)(__bridge void *)request);
            [request finishLoadingWithError:[NSError errorWithDomain:@"HMMedia" code:2 userInfo:nil]];
        }
        [self.pending removeAllObjects];
    });
    dispatch_group_wait(self.workers, DISPATCH_TIME_FOREVER);
}
@end

void *hm_audio_remote(uintptr_t handle, long long total, const char *type) {
    HMResourceLoader *loader = [HMResourceLoader new];
    loader.handle = handle; loader.total = total; loader.type = [NSString stringWithUTF8String:type];
    loader.control = dispatch_queue_create("org.tinglan.media-loader", DISPATCH_QUEUE_SERIAL);
    loader.workers = dispatch_group_create();loader.pending = [NSMutableSet new];
    // Never ask for precise duration/timing on a remote stream: AVFoundation
    // then scans the whole resource to compute it, so a 27 MB lossless FLAC is
    // downloaded end to end before the first sample plays. Duration comes from
    // the catalog, and AVPlayer reports a good estimate once playback starts.
    AVURLAsset *asset = [AVURLAsset URLAssetWithURL:[NSURL URLWithString:@"hwmusic-stream://current/audio"] options:@{}];
    [asset.resourceLoader setDelegate:loader queue:loader.control];
    return hm_audio_from_asset(asset, loader);
}
