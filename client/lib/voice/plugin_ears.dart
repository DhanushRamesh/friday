/// Listening through the platform's recogniser.
library;

import 'dart:async';

import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import 'hushing_the_tone.dart';
import 'listener.dart';
import 'platform_hush.dart';

/// _pauseBeforeStopping : How long a silence ends one stretch of
/// recognition.
///
/// Only Android honours this; the web implementation ignores it entirely
/// and lets Chrome stop whenever it likes. Either way the stretch ending is
/// not the turn ending — VoiceSession starts another — so this is about how
/// often the engine is restarted rather than how long the user may pause.
///
/// Long, because every restart is a new recognition session and Android
/// plays a tone for each one. Always awake, a short pause here is a phone
/// that beeps at an empty room for as long as it is left on.
const Duration _pauseBeforeStopping = Duration(seconds: 12);

/// _harmless : Errors that mean nobody said anything, which always awake
/// produces constantly and by design.
///
/// They were treated as failures, and with cancelOnError the engine stopped
/// on each one. The caller then reopened the microphone, heard nothing
/// again, and stopped again — a loop that recognised nothing and made the
/// phone click at every turn of it.
const Set<String> _harmless = {
  'error_speech_timeout',
  'error_no_match',
  'error_retry',
};

/// _maxSegment : The longest single stretch of recognition. The turn may
/// run over several.
const Duration _maxSegment = Duration(seconds: 60);

/// _restartAttempts : How many times starting is retried before giving up.
///
/// The browser's SpeechRecognition refuses to start while the previous one
/// is still running, and it stays running for a moment after the plugin
/// believes it has stopped. Since always-awake stops and starts on every
/// utterance, that race is hit constantly and a single attempt fails with
/// "recognition has already started".
const int _restartAttempts = 5;

/// _settleBetweenStarts : How long to leave the engine to let go, growing
/// with each attempt.
///
/// A fixed short wait was not enough: the engine sometimes takes most of
/// a second to release, and every attempt failing turned always-awake off
/// with "I could not open the microphone".
const List<Duration> _settleBetweenStarts = [
  Duration(milliseconds: 150),
  Duration(milliseconds: 300),
  Duration(milliseconds: 600),
  Duration(milliseconds: 1000),
  Duration(milliseconds: 1500),
];

/// PluginEars : Listens using speech_to_text — the Web Speech API in a
/// browser, and Android's recogniser on a phone.
class PluginEars implements Ears {
  PluginEars({this.language = 'en-US', HushingTheTone? hush})
    : _hush = hush ?? defaultHushingTheTone();

  /// _hush : Silences the tone the platform plays when a session starts.
  /// Android alone makes one, and offers no way to turn it off.
  final HushingTheTone _hush;

  /// language : Which language and accent model to recognise with.
  final String language;

  final stt.SpeechToText _speech = stt.SpeechToText();
  bool _ready = false;

  /// _opening : Held while a start is in progress.
  ///
  /// Two starts overlapping is the surest way to be refused, and the
  /// caller can reach this from a segment ending and a turn reopening at
  /// nearly the same moment.
  Future<void>? _opening;

  @override
  final ValueListenable<double?> preparing = ValueNotifier<double?>(null);

  /// Android refuses a session begun too soon after the last one, answering
  /// ERROR_RECOGNIZER_BUSY. A quarter of a second was not enough and the
  /// refusals read as the engine being broken.
  /// The platform decides, and decides early.
  @override
  bool get endpointsItself => false;

  @override
  Duration get settleBeforeReopen => Platform.isAndroid
      ? const Duration(milliseconds: 700)
      : const Duration(milliseconds: 250);

  @override
  Future<String?> prepare() async {
    if (_ready) return null;
    try {
      _ready = await _speech.initialize(
        // Reported here rather than thrown. Silence is not a failure, so
        // only the rest is worth noting; what to do about it is decided
        // where the engine is restarted.
        onError: (dynamic e) {
          final name = _errorName(e);
          if (_harmless.contains(name)) return;
          debugPrint('FRIDAY: recogniser error $name');
        },
        onStatus: (String _) {},
      );
    } on Object {
      _ready = false;
    }
    return _ready ? null : 'I cannot use the microphone. Check the permission.';
  }

  @override
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    final problem = await prepare();
    if (problem != null) {
      onError(problem);
      return;
    }

    // Wait for any start already under way rather than racing it.
    final inFlight = _opening;
    if (inFlight != null) await inFlight;

    final opening = Completer<void>();
    _opening = opening.future;
    try {
      await _open(onResult: onResult, onDone: onDone, onError: onError);
    } finally {
      _opening = null;
      opening.complete();
    }
  }

  /// _open : Starts recognition, retrying while the engine is still
  /// letting go of the last one.
  Future<void> _open({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    for (var attempt = 0; attempt < _restartAttempts; attempt++) {
      // Whatever the plugin believes, make sure nothing is running before
      // starting: the engine refuses a second start outright.
      if (_speech.isListening || attempt > 0) {
        try {
          await _speech.cancel();
        } on Object {
          // Nothing was running, which is what we wanted anyway.
        }
        await Future<void>.delayed(_settleBetweenStarts[attempt]);
      }

      try {
        await _hush.around(
          () => _speech.listen(
            onResult: (result) => onResult(
              Heard(result.recognizedWords, settled: result.finalResult),
            ),
            listenOptions: stt.SpeechListenOptions(
              localeId: language.replaceAll('-', '_'),
              // Partial results are what let the words appear as they are
              // spoken, which is the only sign the microphone is working.
              partialResults: true,
              // Silence raises an error, and always awake there is a great
              // deal of silence. Stopping on it is what made the microphone
              // open and shut without ever hearing anything.
              cancelOnError: false,
              listenMode: stt.ListenMode.dictation,
              pauseFor: _pauseBeforeStopping,
              listenFor: _maxSegment,
            ),
          ),
        );
        break;
      } on Object catch (e) {
        debugPrint(
          'FRIDAY: could not start listening (attempt '
          '${attempt + 1}): $e',
        );
        if (attempt == _restartAttempts - 1) {
          // Plain language: the detail is for the log, not for someone
          // who may be hearing this read aloud.
          onError('I could not open the microphone. Try again.');
          return;
        }
      }
    }

    // The engine stops on its own after a pause; this reports that upwards
    // so the caller can send what was heard.
    _speech.statusListener = (status) {
      if (status == 'done' || status == 'notListening') onDone();
    };
  }

  @override
  Future<void> stop() async {
    if (_speech.isListening) await _speech.stop();
  }

  @override
  Future<void> cancel() async {
    if (_speech.isListening) await _speech.cancel();
  }

  @override
  Future<void> dispose() async {
    await cancel();
  }
}

/// _errorName : The plugin's error identifier, whatever shape it arrives in.
String _errorName(dynamic error) {
  try {
    return (error.errorMsg as String?) ?? '';
  } on Object {
    return error.toString();
  }
}
