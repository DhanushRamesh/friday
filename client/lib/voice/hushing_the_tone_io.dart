/// Silencing Android's recognition tone.
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'hushing_the_tone.dart';

/// _channel : Matches the name in MainActivity.
const MethodChannel _channel = MethodChannel('friday/quiet');

/// _longEnoughForTheTone : How long to stay silent after the session has
/// been asked for.
///
/// The tone is played by the recognition service a moment after the request
/// reaches it, which is after the call this wraps has already returned.
/// Long enough to cover that, short enough that a person listening to music
/// hears a gap rather than a silence.
const Duration _longEnoughForTheTone = Duration(milliseconds: 600);

/// TheSystemTone : Silences the streams a recogniser plays its tone through,
/// for as long as it takes to start one.
///
/// Android offers no way to turn the tone off, so the only thing left is to
/// turn the speaker down while it plays. It is blunt: anything else coming
/// out of the phone is silenced too, for about half a second, every time
/// listening begins. On a phone propped on a charger being used as an
/// assistant that is a fair trade. It would not be on a phone playing music.
class TheSystemTone implements HushingTheTone {
  /// _depth : How many calls are inside `around` at once.
  ///
  /// Unmuting happens when the last of them leaves. Two overlapping starts
  /// are possible — a turn ending as another opens — and the first to
  /// finish must not restore the sound while the second is still starting.
  int _depth = 0;

  @override
  Future<T> around<T>(Future<T> Function() opening) async {
    if (!Platform.isAndroid) return opening();

    _depth++;
    if (_depth == 1) await _tell('mute');

    try {
      return await opening();
    } finally {
      // Held past the call because the tone comes from the recognition
      // service, which has not been reached yet when this returns.
      unawaited(
        Future<void>.delayed(_longEnoughForTheTone, () async {
          _depth--;
          if (_depth <= 0) {
            _depth = 0;
            await _tell('unmute');
          }
        }),
      );
    }
  }

  /// _tell : Asks the platform, and carries on if it will not answer.
  ///
  /// A phone that refuses to be quietened is a phone that beeps, which is
  /// worse than it was but not a reason to stop listening.
  Future<void> _tell(String what) async {
    try {
      await _channel.invokeMethod<void>(what);
    } on Object catch (e) {
      debugPrint('FRIDAY: could not $what the recogniser tone: $e');
    }
  }
}

/// createHushingTheTone : What silences the tone on this platform.
HushingTheTone createHushingTheTone() =>
    Platform.isAndroid ? TheSystemTone() : const NoToneToHush();
