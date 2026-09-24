/// Listening in a browser.
///
/// The Web Speech API directly rather than through speech_to_text. The
/// plugin keeps its own idea of whether it is listening, and on the web it
/// drifts from the browser's: it decides it has stopped, so `cancel` does
/// nothing, while the underlying SpeechRecognition is still running and
/// refuses every `start` with "recognition has already started". Retrying
/// cannot fix a cancel that is a no-op.
///
/// Holding the object here means `abort` genuinely stops it, and the
/// lifecycle is ours rather than guessed at.
library;

import 'dart:async';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:flutter/foundation.dart';

import 'listener.dart';

/// createEars : The browser's recogniser.
Ears createEars({String language = 'en-US'}) => WebEars(language: language);

/// _constructor : The SpeechRecognition constructor, under whichever name
/// this browser publishes it.
JSFunction? _constructor() {
  for (final name in ['SpeechRecognition', 'webkitSpeechRecognition']) {
    final found = globalContext.getProperty<JSAny?>(name.toJS);
    if (found != null) return found as JSFunction;
  }
  return null;
}

/// WebEars : Listens through window.SpeechRecognition.
class WebEars implements Ears {
  WebEars({this.language = 'en-US'});

  /// language : What to listen for.
  final String language;

  /// _recognition : The live object, null when nothing is running.
  ///
  /// Its presence is the truth about whether recognition is going, rather
  /// than a flag that can drift from it.
  JSObject? _recognition;

  @override
  Future<String?> prepare() async =>
      _constructor() != null ? null : 'This browser cannot listen.';

  @override
  final ValueListenable<double?> preparing = ValueNotifier<double?>(null);

  /// A fresh SpeechRecognition is built for every listen, so the only wait
  /// is for the previous one to let go of the microphone.
  /// Chrome decides, and decides early.
  @override
  bool get endpointsItself => false;

  @override
  Duration get settleBeforeReopen => const Duration(milliseconds: 250);

  @override
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    // Whatever went before is finished with. abort rather than stop: stop
    // asks for a final result and keeps the object alive for a moment,
    // which is the moment that refuses the next start.
    await _abort();

    final make = _constructor();
    if (make == null) {
      onError('This browser cannot listen.');
      return;
    }

    final recognition = make.callAsConstructor<JSObject>();
    _recognition = recognition;

    recognition
      ..setProperty('lang'.toJS, language.toJS)
      // Keeps going through a pause instead of ending at the first one.
      ..setProperty('continuous'.toJS, true.toJS)
      // Partial results are what let the words appear as they are spoken,
      // which is the only sign the microphone is working.
      ..setProperty('interimResults'.toJS, true.toJS)
      ..setProperty('maxAlternatives'.toJS, 1.toJS);

    recognition.setProperty(
      'onresult'.toJS,
      ((JSObject event) {
        if (!identical(_recognition, recognition)) return;
        final heard = _read(event);
        if (heard != null) onResult(heard);
      }).toJS,
    );

    recognition.setProperty(
      'onend'.toJS,
      ((JSObject _) {
        if (!identical(_recognition, recognition)) return;
        _recognition = null;
        onDone();
      }).toJS,
    );

    recognition.setProperty(
      'onerror'.toJS,
      ((JSObject event) {
        if (!identical(_recognition, recognition)) return;
        final kind =
            event.getProperty<JSString?>('error'.toJS)?.toDart ?? 'unknown';

        // Neither of these is a fault. "no-speech" means a silence went
        // by, and "aborted" means we stopped it ourselves; both end the
        // stretch, and the caller decides whether to open another.
        if (kind == 'no-speech' || kind == 'aborted') {
          _recognition = null;
          onDone();
          return;
        }

        _recognition = null;
        onError(switch (kind) {
          'not-allowed' ||
          'service-not-allowed' => 'I need permission to use the microphone.',
          'audio-capture' => 'I cannot find a microphone.',
          'network' => 'The speech service could not be reached.',
          _ => 'I could not hear anything.',
        });
      }).toJS,
    );

    try {
      recognition.callMethod<JSAny?>('start'.toJS);
    } on Object catch (e) {
      // Should not happen now that the previous object is aborted and
      // dropped, but a start that fails must not leave a dead object
      // behind pretending to be live.
      debugPrint('FRIDAY: SpeechRecognition.start failed: $e');
      _recognition = null;
      onError('I could not open the microphone.');
    }
  }

  /// _read : Pulls the transcript out of a result event.
  ///
  /// Every result is joined rather than only the newest: with continuous
  /// recognition the list accumulates over the stretch, and the caller
  /// wants what has been said so far, not the last fragment of it.
  Heard? _read(JSObject event) {
    final results = event.getProperty<JSObject?>('results'.toJS);
    if (results == null) return null;

    final count = results.getProperty<JSNumber?>('length'.toJS)?.toDartInt ?? 0;
    final words = StringBuffer();
    var settled = false;

    for (var i = 0; i < count; i++) {
      final result = results.getProperty<JSObject?>('$i'.toJS);
      if (result == null) continue;
      final best = result.getProperty<JSObject?>('0'.toJS);
      final text = best?.getProperty<JSString?>('transcript'.toJS)?.toDart;
      if (text == null) continue;
      if (words.isNotEmpty) words.write(' ');
      words.write(text.trim());
      settled = result.getProperty<JSBoolean?>('isFinal'.toJS)?.toDart ?? false;
    }

    return Heard(words.toString().trim(), settled: settled);
  }

  /// _abort : Stops whatever is running and forgets it.
  Future<void> _abort() async {
    final running = _recognition;
    if (running == null) return;
    _recognition = null;

    // Cleared first, so the handlers of the object being thrown away
    // cannot report anything on their way out.
    for (final handler in ['onresult', 'onend', 'onerror']) {
      running.setProperty(handler.toJS, null);
    }
    try {
      running.callMethod<JSAny?>('abort'.toJS);
    } on Object {
      // Already finished, which is the state we were after.
    }
    // A turn of the event loop for the browser to release the microphone.
    await Future<void>.delayed(const Duration(milliseconds: 60));
  }

  @override
  Future<void> stop() async {
    final running = _recognition;
    if (running == null) return;
    try {
      // stop, not abort: it asks for a last result before ending.
      running.callMethod<JSAny?>('stop'.toJS);
    } on Object {
      await _abort();
    }
  }

  @override
  Future<void> cancel() => _abort();

  @override
  Future<void> dispose() => _abort();
}
