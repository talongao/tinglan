#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <AuthenticationServices/AuthenticationServices.h>
#include <stdint.h>

extern void hm_auth_complete(uintptr_t handle, const char *url, int status);
extern int hm_auth_allowed(const char *url);

static void on_main(void (^operation)(void)) {
    if ([NSThread isMainThread]) operation();
    else dispatch_sync(dispatch_get_main_queue(), operation);
}

#pragma mark - System sheet (ASWebAuthenticationSession)

@interface HMAuthorization : NSObject <ASWebAuthenticationPresentationContextProviding>
@property(nonatomic, strong) ASWebAuthenticationSession *session;
@property(nonatomic, strong) NSWindow *anchor;
@property(nonatomic) uintptr_t handle;
@property(nonatomic) BOOL finished;
@end

@implementation HMAuthorization
- (ASPresentationAnchor)presentationAnchorForWebAuthenticationSession:(ASWebAuthenticationSession *)session {
    return self.anchor;
}
- (void)finish:(NSURL *)url status:(int)status {
    if (self.finished) return;
    self.finished = YES;
    hm_auth_complete(self.handle, url.absoluteString.UTF8String, status);
}
@end

void *hm_auth_start(const char *raw, uintptr_t handle) {
    NSString *value = [NSString stringWithUTF8String:raw];
    __block HMAuthorization *owner;
    on_main(^{
        owner = [HMAuthorization new];
        owner.handle = handle;
        owner.anchor = NSApp.keyWindow;
        if (!owner.anchor) {
            [owner finish:nil status:2];
            return;
        }
        __weak HMAuthorization *weakOwner = owner;
        owner.session = [[ASWebAuthenticationSession alloc]
            initWithURL:[NSURL URLWithString:value]
            callbackURLScheme:@"hms"
            completionHandler:^(NSURL *url, NSError *error) {
                on_main(^{ [weakOwner finish:url status:error ? 1 : 0]; });
            }];
        owner.session.presentationContextProvider = owner;
        owner.session.prefersEphemeralWebBrowserSession = YES;
        if (![owner.session start]) [owner finish:nil status:2];
    });
    return (__bridge_retained void *)owner;
}

void hm_auth_close(void *raw) {
    if (!raw) return;
    HMAuthorization *owner = (__bridge_transfer HMAuthorization *)raw;
    on_main(^{
        [owner.session cancel];
        [owner finish:nil status:1];
        owner.session = nil;
        owner.anchor = nil;
    });
}

#pragma mark - Embedded window (WKWebView)

// The embedded window is a disposable browser: non-persistent cookies, no
// downloads, no popups. Navigation stays inside the Huawei login domains; the
// hms:// callback is intercepted and handed back to Go, which validates it.
@interface HMWebAuth : NSObject <NSWindowDelegate, WKNavigationDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) WKWebView *webView;
@property(nonatomic) uintptr_t handle;
@property(nonatomic) BOOL finished;
@end

@implementation HMWebAuth
- (void)finish:(NSURL *)url status:(int)status {
    if (self.finished) return;
    self.finished = YES;
    hm_auth_complete(self.handle, url.absoluteString.UTF8String, status);
}
- (void)webView:(WKWebView *)webView
    decidePolicyForNavigationAction:(WKNavigationAction *)action
                    decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    NSURL *url = action.request.URL;
    NSString *value = url.absoluteString ?: @"";
    if ([url.scheme.lowercaseString isEqualToString:@"hms"]) {
        [self finish:url status:0];
        decisionHandler(WKNavigationActionPolicyCancel);
        return;
    }
    if (hm_auth_allowed(value.UTF8String)) {
        decisionHandler(WKNavigationActionPolicyAllow);
        return;
    }
    decisionHandler(WKNavigationActionPolicyCancel);
}
- (void)windowWillClose:(NSNotification *)notification {
    [self finish:nil status:1];
}
@end

void *hm_web_auth_start(const char *raw, uintptr_t handle) {
    NSString *value = [NSString stringWithUTF8String:raw];
    __block HMWebAuth *owner;
    on_main(^{
        if (!NSApp) return;
        // AppKit's automatic termination fires a quit request as soon as the
        // last visible window closes. The auth window is transient, so keep
        // termination disabled until it is gone; otherwise closing it during
        // a login tears the app down and the session is never persisted.
        [[NSProcessInfo processInfo] disableAutomaticTermination:@"tinglan-login"];
        owner = [HMWebAuth new];
        owner.handle = handle;
        NSRect frame = NSMakeRect(0, 0, 820, 720);
        NSWindow *window = [[NSWindow alloc]
            initWithContentRect:frame
                      styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable)
                        backing:NSBackingStoreBuffered
                          defer:NO];
        window.title = @"听澜 · 华为账号登录";
        // ARC owns this window; releasedWhenClosed would over-release it.
        window.releasedWhenClosed = NO;
        window.delegate = owner;
        WKWebViewConfiguration *config = [WKWebViewConfiguration new];
        config.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
        config.preferences.javaScriptCanOpenWindowsAutomatically = NO;
        WKWebView *view = [[WKWebView alloc] initWithFrame:frame configuration:config];
        view.navigationDelegate = owner;
        window.contentView = view;
        owner.window = window;
        owner.webView = view;
        [window center];
        [window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
        [view loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:value]]];
    });
    if (!owner) return NULL;
    return (__bridge_retained void *)owner;
}

void hm_web_auth_close(void *raw) {
    if (!raw) return;
    HMWebAuth *owner = (__bridge_transfer HMWebAuth *)raw;
    // Never tear the window down inside a WKWebView delegate callback: the
    // navigation machinery is still on the stack. Hop once so the delegate
    // has returned before the window and its web view go away.
    dispatch_async(dispatch_get_main_queue(), ^{
        [owner finish:nil status:1];
        [owner.webView stopLoading];
        owner.webView.navigationDelegate = nil;
        owner.window.delegate = nil;
        [owner.window orderOut:nil];
        [owner.window close];
        owner.webView = nil;
        owner.window = nil;
        [[NSProcessInfo processInfo] enableAutomaticTermination:@"tinglan-login"];
        // Bring the main window back so the app does not look like it quit.
        for (NSWindow *candidate in NSApp.windows) {
            if (candidate != owner.window && candidate.isVisible && candidate.canBecomeKeyWindow) {
                [candidate makeKeyAndOrderFront:nil];
                break;
            }
        }
        [NSApp activateIgnoringOtherApps:YES];
    });
}
