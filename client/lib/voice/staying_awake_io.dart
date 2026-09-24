/// Keeping the microphone alive on a phone.
library;

import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';
import 'package:wakelock_plus/wakelock_plus.dart';

import 'staying_awake.dart';

/// _channel : Where the notification lives in the phone's settings, so the
/// user can find it and see what it is for.
const String _channel = 'friday_listening';

/// AForegroundService : What Android needs held for FRIDAY to keep
/// listening.
///
/// Two things, for two different ways of being used. A phone left on a
/// charger with the app in front of it needs the screen kept awake, or
/// Android dims and sleeps it and the microphone goes with it. A phone in
/// a pocket needs a foreground service, which is the only way an app that
/// is not on screen may hold the microphone at all.
///
/// The service comes with a notification that cannot be dismissed while it
/// runs. That is not a detail to work around: it is the bargain Android
/// offers, and an assistant that listens all day should be visibly doing
/// so.
class AForegroundService implements StayingAwake {
  @override
  Future<String?> begin() async {
    if (!Platform.isAndroid) return null;

    // The screen first, because it is the case that actually gets used:
    // the phone propped on a charger being talked to. It is also the one
    // that cannot fail.
    try {
      await WakelockPlus.enable();
    } on Object catch (e) {
      debugPrint('FRIDAY: could not keep the screen awake: $e');
    }

    if (!await FlutterForegroundTask.canDrawOverlays) {
      // Not required, and not worth refusing over. Noted only because its
      // absence changes what a tap on the notification can do.
      debugPrint('FRIDAY: no overlay permission; the notification still works');
    }

    final allowed = await FlutterForegroundTask.requestNotificationPermission();
    if (allowed != NotificationPermission.granted) {
      return 'I need permission to show a notification before I can keep '
          'listening with the screen off.';
    }

    FlutterForegroundTask.init(
      androidNotificationOptions: AndroidNotificationOptions(
        channelId: _channel,
        channelName: 'Listening',
        channelDescription: 'Shown while FRIDAY is listening for its name.',
        // Silent and low: this notification is a disclosure, not an event.
        // One that pinged or vibrated would be unbearable.
        channelImportance: NotificationChannelImportance.LOW,
        priority: NotificationPriority.LOW,
        playSound: false,
        enableVibration: false,
        onlyAlertOnce: true,
      ),
      iosNotificationOptions: const IOSNotificationOptions(),
      foregroundTaskOptions: ForegroundTaskOptions(
        // Nothing runs in the service isolate. It exists so the process
        // keeps the microphone; the listening itself stays where it is,
        // in the app, where the conversation and the speaker already live.
        eventAction: ForegroundTaskEventAction.nothing(),
        allowWakeLock: true,
        autoRunOnBoot: false,
      ),
    );

    try {
      if (await FlutterForegroundTask.isRunningService) return null;

      final started = await FlutterForegroundTask.startService(
        serviceTypes: [ForegroundServiceTypes.microphone],
        notificationTitle: 'FRIDAY is listening',
        notificationText: 'Say "FRIDAY" to ask something.',
      );
      if (started is ServiceRequestFailure) {
        debugPrint('FRIDAY: service would not start: ${started.error}');
        return 'I could not keep listening in the background.';
      }
    } on Object catch (e) {
      debugPrint('FRIDAY: service would not start: $e');
      return 'I could not keep listening in the background.';
    }
    return null;
  }

  @override
  Future<void> end() async {
    if (!Platform.isAndroid) return;

    try {
      await WakelockPlus.disable();
    } on Object catch (e) {
      debugPrint('FRIDAY: could not release the screen: $e');
    }

    try {
      if (await FlutterForegroundTask.isRunningService) {
        await FlutterForegroundTask.stopService();
      }
    } on Object catch (e) {
      debugPrint('FRIDAY: service would not stop: $e');
    }
  }
}

/// createStayingAwake : What this platform needs held.
StayingAwake createStayingAwake() =>
    Platform.isAndroid ? AForegroundService() : const NothingToHold();
