/// Deciding when someone has stopped talking.
library;

import 'dart:math' as math;

import 'package:flutter/foundation.dart';

/// Ending : Why a stretch of listening finished.
enum Ending {
  /// none : It has not.
  none,

  /// spoke : Something was said and the speaker has stopped.
  spoke,

  /// silence : Nothing was said at all.
  silence,
}

/// Endpointer : Watches the loudness of recorded audio and reports when the
/// speaker has finished.
///
/// Whisper's own gate keeps silence away from the decoder but says nothing
/// about whose turn it is, and the recogniser this replaced would not say
/// either: it stopped when it felt like it, which is how a sentence came to
/// be cut in half. Deciding here means the rule is one place and can be
/// tested without a microphone.
class Endpointer {
  Endpointer({
    required this.sampleRate,
    this.pauseAfterSpeech = const Duration(milliseconds: 800),
    this.patienceBeforeSpeech = const Duration(seconds: 15),
    this.voiceFloor = 0.0015,
    this.voiceOverNoise = 2.5,
    this.noiseFloorCap = 0.01,
  }) : _noiseFloor = noiseFloorCap;

  /// sampleRate : Samples per second, which is how a chunk's length in
  /// bytes becomes a length in time.
  final int sampleRate;

  /// pauseAfterSpeech : How long a silence ends the stretch once something
  /// has been said.
  ///
  /// This is not the whole wait before an answer is asked for: VoiceSession
  /// waits again once the text arrives, so the two are spent in series and
  /// both have to be short.
  final Duration pauseAfterSpeech;

  /// patienceBeforeSpeech : How long to wait for a first word before giving
  /// up, so the caller can close the microphone and open it again rather
  /// than listening to an empty room for ever.
  final Duration patienceBeforeSpeech;

  /// voiceFloor : The quietest chunk that can count as speech, as RMS of
  /// samples scaled to ±1. Below this is a quiet room, not a quiet talker.
  final double voiceFloor;

  /// voiceOverNoise : How far above the measured noise floor a chunk must
  /// rise to count as speech, so a fan or a fridge raises the bar rather
  /// than being heard as talking.
  final double voiceOverNoise;

  /// noiseFloorCap : The loudest the room may be considered quiet. Without
  /// a cap an early burst of speech would raise the floor so far that
  /// nothing after it registers.
  final double noiseFloorCap;

  double _noiseFloor;
  Duration _quiet = Duration.zero;
  bool _spoke = false;

  /// spoke : Whether anything has been heard yet.
  bool get spoke => _spoke;

  /// noiseFloor : What the room is currently reckoned to sound like.
  @visibleForTesting
  double get noiseFloor => _noiseFloor;

  /// hear : Takes one chunk of 16-bit mono audio and says whether the
  /// stretch should end.
  Ending hear(Uint8List chunk) {
    final samples = chunk.lengthInBytes ~/ 2;
    if (samples == 0) return Ending.none;

    final pcm = chunk.buffer.asInt16List(chunk.offsetInBytes, samples);
    var sum = 0.0;
    for (final sample in pcm) {
      final scaled = sample / 32768.0;
      sum += scaled * scaled;
    }
    final rms = math.sqrt(sum / samples);
    final span = Duration(microseconds: samples * 1000000 ~/ sampleRate);

    if (rms > math.max(_noiseFloor * voiceOverNoise, voiceFloor)) {
      _spoke = true;
      _quiet = Duration.zero;
      return Ending.none;
    }

    // Quiet: let the floor settle towards what the room actually sounds
    // like, so the bar for speech tracks the surroundings.
    _noiseFloor = math.min(noiseFloorCap, _noiseFloor * 0.95 + rms * 0.05);
    _quiet += span;

    if (_quiet < (_spoke ? pauseAfterSpeech : patienceBeforeSpeech)) {
      return Ending.none;
    }
    return _spoke ? Ending.spoke : Ending.silence;
  }
}
