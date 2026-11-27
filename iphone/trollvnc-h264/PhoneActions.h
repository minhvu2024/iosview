#import <Foundation/Foundation.h>
#import <dlfcn.h>
#import <rfb/rfb.h>

// Included by trollvncserver.mm, after gViewOnly is declared.
@protocol IVWorkspace
+ (id<IVWorkspace>)defaultWorkspace;
- (NSArray *)allInstalledApplications;
- (BOOL)openApplicationWithBundleID:(NSString *)identifier;
@end

@protocol IVApp
- (NSString *)applicationIdentifier;
- (NSString *)localizedName;
- (NSArray *)appTags;
@end

@protocol IVSpringBoard
+ (id<IVSpringBoard>)server;
- (BOOL)showControlCenter:(BOOL)visible;
@end

static NSDictionary *ivPhoneAction(NSDictionary *request) {
    NSString *action = request[@"action"];
    if ([action isEqual:@"cc"]) {
        static dispatch_once_t once;
        dispatch_once(&once, ^{
            dlopen("/System/Library/PrivateFrameworks/AccessibilityUtilities.framework/AccessibilityUtilities", RTLD_LAZY);
        });
        Class api = NSClassFromString(@"AXSpringBoardServer");
        id server = [api respondsToSelector:@selector(server)] ? [(Class<IVSpringBoard>)api server] : nil;
        if ([server respondsToSelector:@selector(showControlCenter:)] && [(id<IVSpringBoard>)server showControlCenter:YES])
            return @{};
        return @{@"error": @"iOS chưa mở được Trung tâm điều khiển"};
    }

    Class api = NSClassFromString(@"LSApplicationWorkspace");
    id workspace = [api respondsToSelector:@selector(defaultWorkspace)] ? [(Class<IVWorkspace>)api defaultWorkspace] : nil;
    if ([action isEqual:@"apps"] && [workspace respondsToSelector:@selector(allInstalledApplications)]) {
        NSMutableArray *apps = [NSMutableArray array];
        for (id proxy in [(id<IVWorkspace>)workspace allInstalledApplications]) {
            if (![proxy respondsToSelector:@selector(applicationIdentifier)] || ![proxy respondsToSelector:@selector(localizedName)]) continue;
            if ([proxy respondsToSelector:@selector(appTags)] && [[(id<IVApp>)proxy appTags] containsObject:@"hidden"]) continue;
            NSString *identifier = [(id<IVApp>)proxy applicationIdentifier];
            NSString *name = [(id<IVApp>)proxy localizedName];
            if (identifier.length && name.length) [apps addObject:@{@"id": identifier, @"name": name}];
        }
        return @{@"apps": apps};
    }
    if ([action isEqual:@"open"] && [workspace respondsToSelector:@selector(openApplicationWithBundleID:)]) {
        NSString *identifier = request[@"bundle"];
        if ([identifier isKindOfClass:NSString.class] && identifier.length && identifier.length <= 255 &&
            [(id<IVWorkspace>)workspace openApplicationWithBundleID:identifier]) return @{};
        return @{@"error": @"Ứng dụng chưa cài hoặc iOS từ chối mở"};
    }
    return @{@"error": @"iOS chưa hỗ trợ thao tác này"};
}

// Reuse the authenticated RFB TextChat framing. No clipboard or video traffic.
static void ivTextAction(rfbClientPtr cl, int length, char *bytes) {
    if (!bytes || length <= 0 || length > 2048) return;
    @autoreleasepool {
        NSData *raw = [NSData dataWithBytes:bytes length:(NSUInteger)length];
        NSDictionary *request = [NSJSONSerialization JSONObjectWithData:raw options:0 error:nil];
        if (![request isKindOfClass:NSDictionary.class] || ![request[@"seq"] isKindOfClass:NSNumber.class]) return;
        NSDictionary *result;
        @try {
            result = (cl->viewOnly || gViewOnly) ? @{@"error": @"Máy đang ở chế độ chỉ xem"} : ivPhoneAction(request);
        } @catch (NSException *exception) {
            result = @{@"error": @"iOS từ chối thao tác"};
        }
        NSMutableDictionary *reply = [result mutableCopy];
        reply[@"seq"] = request[@"seq"];
        NSData *data = [NSJSONSerialization dataWithJSONObject:reply options:0 error:nil];
        if (data.length > 512 * 1024) {
            data = [NSJSONSerialization dataWithJSONObject:@{@"seq":request[@"seq"], @"error":@"Danh sách ứng dụng quá lớn"} options:0 error:nil];
        }
        rfbTextChatMsg header = {};
        header.type = rfbTextChat;
        header.length = htonl((uint32_t)data.length);
        NSMutableData *packet = [NSMutableData dataWithBytes:&header length:sz_rfbTextChatMsg];
        [packet appendData:data];
        LOCK(cl->sendMutex);
        int written = rfbWriteExact(cl, (const char *)packet.bytes, (int)packet.length);
        UNLOCK(cl->sendMutex);
        if (written < 0) rfbCloseClient(cl);
    }
}
