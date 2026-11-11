#import <AppKit/AppKit.h>

// Terminal opens the bundled command file through Launch Services, so launching
// the app does not need permission to control Terminal through Apple Events.
int main(void) {
    @autoreleasepool {
        NSApplication *application = NSApplication.sharedApplication;
        [application setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSURL *command = [NSBundle.mainBundle.resourceURL URLByAppendingPathComponent:@"Player.command"];
        NSURL *terminal = [NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:@"com.apple.Terminal"];
        if (!terminal) {
            NSAlert *alert = [[NSAlert alloc] init];
            alert.messageText = @"无法打开木末 Molpe";
            alert.informativeText = @"请检查系统的终端应用是否可用。";
            [alert runModal];
            return 1;
        }
        NSWorkspaceOpenConfiguration *configuration = [NSWorkspaceOpenConfiguration configuration];
        [NSWorkspace.sharedWorkspace openURLs:@[command]
                           withApplicationAtURL:terminal
                                  configuration:configuration
                              completionHandler:^(NSRunningApplication *app, NSError *error) {
            dispatch_async(dispatch_get_main_queue(), ^{
                if (error) {
                    NSAlert *alert = [[NSAlert alloc] init];
                    alert.messageText = @"无法打开木末 Molpe";
                    alert.informativeText = error.localizedDescription;
                    [alert runModal];
                }
                [application terminate:nil];
            });
        }];
        [application run];
    }
    return 0;
}
