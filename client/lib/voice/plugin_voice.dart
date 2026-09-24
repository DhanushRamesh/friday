/// Speaking through the platform's synthesiser.
library;

import 'dart:async';

import 'package:flutter_tts/flutter_tts.dart';

import 'speaker.dart';

/// PluginVoice : Speaks using flutter_tts — the Web Speech API in a
/// browser, and Android's own synthesiser on a phone.
class PluginVoice implements Voice {
  PluginVoice({this.language = 'en-GB', this.rate = 0.52, this.pitch = 1.0});

  /// language : Which voice to ask for. The engine falls back to whatever
  /// it has if this one is absent.
  final String language;

  /// rate : How fast to speak.
  ///
  /// Slower than the default, which gabbles. This is being listened to
  /// rather than read, often through earbuds while doing something else,
  /// and a sentence missed cannot be glanced at again.
  final double rate;

  final double pitch;

  final FlutterTts _tts = FlutterTts();
  bool _configured = false;

  /// _finished : Completes when the engine says it has stopped speaking.
  Completer<void>? _finished;

  /// _configure : Sets the voice up once, and wires the handlers that tell
  /// us when an utterance is done.
  Future<void> _configure() async {
    if (_configured) return;
    _configured = true;

    await _tts.setLanguage(language);
    await _tts.setSpeechRate(rate);
    await _tts.setPitch(pitch);
    await _tts.setVolume(1);

    // Android can await speech completion directly; the web cannot, so the
    // handlers below are what makes `say` finish at the right moment on
    // both.
    try {
      await _tts.awaitSpeakCompletion(true);
    } on Object {
      // Not supported everywhere, and the handlers cover it.
    }

    _tts.setCompletionHandler(_settle);
    _tts.setCancelHandler(_settle);
    _tts.setErrorHandler((dynamic _) => _settle());
  }

  /// _settle : Releases whoever is waiting for the current utterance.
  void _settle() {
    final pending = _finished;
    _finished = null;
    if (pending != null && !pending.isCompleted) pending.complete();
  }

  @override
  Future<bool> available() async {
    try {
      await _configure();
    } on Object {
      return false;
    }

    // Chrome fills in its voices asynchronously, so asking once at startup
    // reliably returns an empty list and FRIDAY comes up silently muted.
    // Polling briefly is what the browser's own voiceschanged event would
    // tell us, without needing a platform-specific listener.
    for (var attempt = 0; attempt < 10; attempt++) {
      try {
        final languages = await _tts.getLanguages;
        if (languages is List && languages.isNotEmpty) return true;
      } on Object {
        return false;
      }
      await Future<void>.delayed(const Duration(milliseconds: 150));
    }
    return false;
  }

  @override
  Future<void> say(String text) async {
    await _configure();

    // Settle anything still outstanding, so a previous utterance cannot
    // leave a waiter stuck for ever.
    _settle();

    final finished = Completer<void>();
    _finished = finished;
    await _tts.speak(text);

    // A cap, because a synthesiser that never reports completion would
    // otherwise stall the queue permanently. Generous enough for a long
    // answer read slowly.
    await finished.future.timeout(_estimate(text), onTimeout: () {});
  }

  /// _estimate : Roughly how long this should take to say, plus slack.
  ///
  /// Used only as the deadline for a completion that never arrives, so
  /// being approximate is fine; being too short would cut off a long
  /// answer.
  Duration _estimate(String text) {
    // About twelve characters a second at this rate, then doubled.
    final seconds = (text.length / 12 * 2).clamp(5, 300).round();
    return Duration(seconds: seconds);
  }

  @override
  Future<void> silence() async {
    try {
      await _tts.stop();
    } on Object {
      // Nothing to stop.
    }
    _settle();
  }

  @override
  Future<void> dispose() async {
    await silence();
  }
}
