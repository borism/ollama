#import "cluster_menu_darwin.h"

static NSMenuItem *clusterModeItem;
static NSMenuItem *clusterShareItem;
static NSMenuItem *clusterPlacementItem;
static NSMenuItem *clusterWaterfillItem;
static NSMenuItem *clusterGreedyItem;

// A filled status dot for the menu's state column, where the checkmark
// would go. Not a template image, so AppKit keeps its color.
static NSImage *clusterDot(NSColor *color) {
    return [NSImage imageWithSize:NSMakeSize(10, 10)
                          flipped:NO
                   drawingHandler:^BOOL(NSRect rect) {
                       [color setFill];
                       [[NSBezierPath bezierPathWithOvalInRect:NSInsetRect(rect, 1.5, 1.5)] fill];
                       return YES;
                   }];
}

// Green when the setting is on, red when off.
static NSMenuItem *clusterItem(NSString *title, SEL action, id target) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:@""];
    [item setTarget:target];
    [item setOnStateImage:clusterDot([NSColor systemGreenColor])];
    [item setOffStateImage:clusterDot([NSColor systemRedColor])];
    return item;
}

// "On"/"Off", or why the menu can't change it.
static NSString *clusterToolTip(bool on, bool locked, NSString *envVar) {
    if (locked) {
        return [NSString stringWithFormat:@"%@ (set by the %@ environment variable)", on ? @"On" : @"Off", envVar];
    }
    return on ? @"On" : @"Off";
}

@implementation AppDelegate (Cluster)

- (void)addClusterMenuItemsTo:(NSMenu *)menu {
    [menu addItem:[NSMenuItem separatorItem]];
    clusterModeItem = clusterItem(@"Cluster Mode", @selector(toggleClusterMode:), self);
    [menu addItem:clusterModeItem];
    clusterShareItem = clusterItem(@"Share This Computer's GPU", @selector(toggleClusterShare:), self);
    [menu addItem:clusterShareItem];

    // OLLAMA_CLUSTER_PLACEMENT: a pick-one submenu, checkmarked like any other.
    NSMenu *placement = [[NSMenu alloc] init];
    clusterWaterfillItem = [placement addItemWithTitle:@"Waterfill"
                                                action:@selector(pickClusterPlacement:)
                                         keyEquivalent:@""];
    [clusterWaterfillItem setTarget:self];
    [clusterWaterfillItem setToolTip:@"Weigh peers by load and latency; can spread a model across several"];
    clusterGreedyItem = [placement addItemWithTitle:@"Greedy"
                                             action:@selector(pickClusterPlacement:)
                                      keyEquivalent:@""];
    [clusterGreedyItem setTarget:self];
    [clusterGreedyItem setToolTip:@"Use the peers with the most free memory, as few as possible"];
    clusterPlacementItem = [[NSMenuItem alloc] initWithTitle:@"Placement" action:nil keyEquivalent:@""];
    [clusterPlacementItem setSubmenu:placement];
    [menu addItem:clusterPlacementItem];

    [self refreshClusterMenuState];
}

// Called on menuWillOpen, so a change made in Settings or with
// `ollama cluster` shows up here too.
- (void)refreshClusterMenuState {
    bool enabled = false, share = false, greedy = false;
    bool enabledLocked = false, shareLocked = false, placementLocked = false;
    BOOL reachable = ClusterMenuState(&enabled, &share, &greedy, &enabledLocked, &shareLocked, &placementLocked);

    [clusterModeItem setState:enabled ? NSControlStateValueOn : NSControlStateValueOff];
    [clusterShareItem setState:share ? NSControlStateValueOn : NSControlStateValueOff];
    [clusterModeItem setToolTip:clusterToolTip(enabled, enabledLocked, @"OLLAMA_CLUSTER")];
    [clusterShareItem setToolTip:clusterToolTip(share, shareLocked, @"OLLAMA_CLUSTER_SHARE")];
    [clusterPlacementItem setToolTip:placementLocked ? @"Set by the OLLAMA_CLUSTER_PLACEMENT environment variable" : nil];
    char *gpu = ClusterGPUName();
    NSString *gpuName = [NSString stringWithUTF8String:gpu];
    free(gpu);
    [clusterShareItem setTitle:gpuName.length ? [@"Share the " stringByAppendingString:gpuName]
                                              : @"Share This Computer's GPU"];
    [clusterWaterfillItem setState:greedy ? NSControlStateValueOff : NSControlStateValueOn];
    [clusterGreedyItem setState:greedy ? NSControlStateValueOn : NSControlStateValueOff];

    // Sharing and placement only mean something while cluster mode is on
    // (same as Settings). Nothing is clickable until the server answers,
    // or when an environment variable decides the setting.
    [clusterModeItem setEnabled:reachable && !enabledLocked];
    [clusterShareItem setEnabled:reachable && enabled && !shareLocked];
    [clusterPlacementItem setEnabled:reachable && enabled && !placementLocked];
}

- (void)toggleClusterMode:(NSMenuItem *)sender {
    SetClusterModeEnabled([sender state] != NSControlStateValueOn);
    [self refreshClusterMenuState];
}

- (void)toggleClusterShare:(NSMenuItem *)sender {
    SetClusterShareEnabled([sender state] != NSControlStateValueOn);
    [self refreshClusterMenuState];
}

- (void)pickClusterPlacement:(NSMenuItem *)sender {
    if ([sender state] != NSControlStateValueOn) {
        SetClusterPlacementGreedy(sender == clusterGreedyItem);
    }
    [self refreshClusterMenuState];
}

@end
