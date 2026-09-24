/// Speaking in a browser.
///
/// The Web Speech API directly rather than through flutter_tts, for two
/// reasons. The plugin's `getLanguages` reported none on a browser with
/// nineteen voices installed, so anything deciding on it was wrong; and
/// driving the API here means owning the two things that matter — picking
/// a voice for the language rather than taking the browser's default, and
/// finishing `say` on the utterance's own end event so the queue runs one
/// at a time. Android keeps the plugin, where it is solid.
library;

import 'dart:async';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:flutter/foundation.dart';
import 'package:web/web.dart' as web;

import 'speaker.dart';

/// createVoice : The browser's synthesiser.
Voice createVoice({String language = 'en-US', double rate = 0.95}) =>
    WebVoice(language: language, rate: rate);

/// WebVoice : Speaks through window.speechSynthesis.
class WebVoice implements Voice {
  WebVoice({this.language = 'en-US', this.rate = 0.95});

  /// language : Which voice to prefer.
  final String language;

  /// rate : How fast to speak, where one is the browser's normal pace.
  ///
  /// Barely slowed: the browser's default is already measured, and this is
  /// listened to rather than read.
  final double rate;

  /// _finished : Completes when the current utterance ends, however it
  /// ends.
  Completer<void>? _finished;

  @override
  Future<bool> available() async {
    final synthesis = web.window.speechSynthesis;
    // Chrome fills its voice list asynchronously, so asking once at
    // startup reliably answers "none" on a machine that speaks fine.
    for (var attempt = 0; attempt < 10; attempt++) {
      if (synthesis.getVoices().toDart.isNotEmpty) return true;
      await Future<void>.delayed(const Duration(milliseconds: 150));
    }
    return false;
  }

  @override
  Future<void> say(String text) async {
    // Release anything outstanding, so a previous utterance cannot leave
    // the queue waiting for ever.
    _settle();

    // The very first utterance after a page loads would otherwise find no
    // voices and be spoken in the browser's default.
    await _awaitVoices();

    final utterance = web.SpeechSynthesisUtterance(text)
      ..lang = language
      ..rate = rate
      ..volume = 1;

    _pickVoice(utterance);

    final finished = Completer<void>();
    _finished = finished;
    String? refusal;

    // end and error both mean this utterance is over; without handling
    // error, one that fails would stall everything queued behind it.
    utterance.onend = ((web.Event _) => _settle()).toJS;
    utterance.onerror = ((JSObject event) {
      final kind = event.getProperty<JSString?>('error'.toJS)?.toDart;
      refusal = switch (kind) {
        // The browser refuses to play audio on a page nobody has
        // touched. Asking by voice involves no click or keypress, so
        // this is exactly the case that hits it — and it fails in total
        // silence, which is indistinguishable from the volume being
        // down.
        'not-allowed' =>
          'Your browser will not let me speak until you click the page '
              'once. Click anywhere and ask again.',
        'audio-busy' => 'Something else is using the speaker.',
        'synthesis-unavailable' ||
        'synthesis-failed' => 'This browser has no voice available.',
        'language-unavailable' ||
        'voice-unavailable' => 'There is no voice installed for that language.',
        // Stopping on purpose reports as an error too, and is not one.
        'interrupted' || 'canceled' => null,
        _ => null,
      };
      if (kind != null) debugPrint('FRIDAY: synthesiser said "$kind"');
      _settle();
    }).toJS;

    debugPrint(
      'FRIDAY: speaking ${text.length} chars as '
      '${utterance.voice?.name ?? "the default voice"}',
    );
    web.window.speechSynthesis.speak(utterance);

    // A cap, because an utterance that never reports its end would
    // otherwise stop FRIDAY speaking again for the rest of the session.
    await finished.future.timeout(_estimate(text), onTimeout: _settle);

    final reason = refusal;
    if (reason != null) throw VoiceRefused(reason);
  }

  /// _awaitVoices : Waits briefly for the browser to publish its voices.
  ///
  /// Chrome fills the list asynchronously, so the first utterance after a
  /// page loads finds it empty and is spoken in whatever default the
  /// browser fancies — which is how an answer came out in the wrong
  /// accent while every later one was right.
  Future<void> _awaitVoices() async {
    for (var attempt = 0; attempt < 12; attempt++) {
      if (web.window.speechSynthesis.getVoices().toDart.isNotEmpty) return;
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
  }

  /// _pickVoice : Chooses a voice matching the language, if one is there.
  ///
  /// Without this the browser picks its default, which on a machine with
  /// several languages installed can read English in a German accent.
  void _pickVoice(web.SpeechSynthesisUtterance utterance) {
    final voices = web.window.speechSynthesis.getVoices().toDart;
    if (voices.isEmpty) return;

    final wanted = language.toLowerCase();
    final prefix = wanted.split('-').first;

    for (final voice in voices) {
      if (voice.lang.toLowerCase() == wanted) {
        utterance.voice = voice;
        return;
      }
    }
    for (final voice in voices) {
      if (voice.lang.toLowerCase().startsWith(prefix)) {
        utterance.voice = voice;
        return;
      }
    }
  }

  /// _estimate : Roughly how long this should take, plus generous slack.
  Duration _estimate(String text) =>
      Duration(seconds: (text.length / 12 * 2).clamp(5, 300).round());

  /// _settle : Releases whoever is waiting for the current utterance.
  void _settle() {
    final pending = _finished;
    _finished = null;
    if (pending != null && !pending.isCompleted) pending.complete();
  }

  @override
  Future<void> silence() async {
    web.window.speechSynthesis.cancel();
    _settle();
  }

  @override
  Future<void> dispose() async => silence();
}
