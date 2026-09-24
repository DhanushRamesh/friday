/// Saying things aloud.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

/// Utterance : One thing to say.
@immutable
class Utterance {
  const Utterance(this.text, {this.interrupts = false});

  final String text;

  /// interrupts : Whether this clears whatever is queued and speaks at
  /// once. Used for a refusal or a failure, which is no longer true to
  /// follow with the progress that preceded it.
  final bool interrupts;

  @override
  String toString() => 'Utterance(${text.length} chars)';
}

/// VoiceRefused : The engine would not speak, and said why.
///
/// Distinct from an ordinary failure because the usual cause is a
/// browser refusing to play audio on a page nobody has touched, which
/// the user can put right — but only if they are told.
class VoiceRefused implements Exception {
  const VoiceRefused(this.reason);

  /// reason : What to show. Written for a person.
  final String reason;

  @override
  String toString() => 'VoiceRefused: $reason';
}

/// Voice : The engine that turns text into sound.
///
/// An interface so the queue above it can be tested without audio, and so
/// Android can use something other than the browser's synthesiser later.
abstract interface class Voice {
  /// available : Whether this device can speak at all.
  Future<bool> available();

  /// say : Speaks text, completing when it has finished being spoken.
  ///
  /// Completing on finish is what lets the queue speak one thing at a time;
  /// an engine that returns immediately would talk over itself.
  Future<void> say(String text);

  /// silence : Stops mid-sentence.
  Future<void> silence();

  /// dispose : Releases the engine.
  Future<void> dispose();
}

/// Speaker : Speaks what FRIDAY says, one thing at a time.
///
/// A queue rather than a bare call, because messages arrive faster than
/// they can be spoken: FRIDAY says "Let me look into that" and the answer
/// may land before that sentence is finished. Without a queue the second
/// cuts off the first, which is exactly the thing that makes a voice
/// assistant sound broken.
class Speaker extends ChangeNotifier {
  Speaker(this._voice, {this.enabled = true});

  final Voice _voice;

  /// enabled : Whether to speak at all. A user reading on a laptop wants
  /// the text and not the sound.
  bool enabled;

  /// problem : Why nothing is being heard, if that is the case.
  ///
  /// A synthesiser that refuses in silence is indistinguishable from one
  /// that is working while the volume is down, and the user has no way
  /// to tell which. Whatever it says is kept here to be shown.
  String? problem;

  final List<Utterance> _queue = [];
  bool _speaking = false;
  bool _disposed = false;
  String _nowSaying = '';

  /// speaking : Whether something is being said right now.
  bool get speaking => _speaking;

  /// nowSaying : The words being spoken at this moment, empty when
  /// silent.
  ///
  /// Read by whoever is deciding whether the microphone is hearing the
  /// user or hearing FRIDAY.
  String get nowSaying => _nowSaying;

  /// pending : How much is waiting to be said.
  int get pending => _queue.length;

  /// say : Adds something to be spoken.
  ///
  /// Empty text is dropped rather than queued: a message with nothing in it
  /// would otherwise produce a pause for no reason.
  void say(String text, {bool interrupts = false}) {
    if (!enabled || _disposed) return;
    final trimmed = text.trim();
    if (trimmed.isEmpty) return;

    debugPrint('FRIDAY: say "${_short(trimmed)}"');
    if (interrupts) {
      _queue.clear();
      unawaited(_voice.silence());
    }
    _queue.add(Utterance(trimmed, interrupts: interrupts));
    notifyListeners();
    unawaited(_drain());
  }

  /// silence : Stops speaking and forgets what was queued.
  ///
  /// What "stop" does, and what superseding a prompt does: the answer being
  /// read is no longer wanted, and neither is the rest of it.
  Future<void> silence() async {
    if (_speaking || _queue.isNotEmpty) {
      debugPrint('FRIDAY: silenced while saying "${_short(_nowSaying)}"');
    }
    _queue.clear();
    _nowSaying = '';
    notifyListeners();
    await _voice.silence();
  }

  /// setEnabled : Turns speech on or off, silencing it when turned off.
  Future<void> setEnabled(bool value) async {
    if (enabled == value) return;
    enabled = value;
    if (!value) await silence();
    notifyListeners();
  }

  /// _drain : Speaks the queue in order, until it is empty.
  Future<void> _drain() async {
    if (_speaking || _disposed) return;
    _speaking = true;
    notifyListeners();

    while (_queue.isNotEmpty && !_disposed) {
      final next = _queue.removeAt(0);
      _nowSaying = next.text;
      try {
        await _voice.say(next.text);
        problem = null;
      } on VoiceRefused catch (refusal) {
        // Shown rather than swallowed. Speech failing without a word
        // said about it is the worst of both: nothing is heard and there
        // is nothing to act on.
        problem = refusal.reason;
        debugPrint('FRIDAY: speaking refused: ${refusal.reason}');
      } on Object catch (e, stack) {
        // A synthesiser that fails must not stop the conversation — the
        // answer is on screen to read.
        problem = 'I could not speak that aloud.';
        debugPrint('FRIDAY: speaking failed: $e\n$stack');
      }
    }

    _nowSaying = '';
    _speaking = false;
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _queue.clear();
    unawaited(_voice.silence());
    unawaited(_voice.dispose());
    super.dispose();
  }
}

/// SilentVoice : A voice that says nothing, for a device that cannot speak
/// and for tests.
class SilentVoice implements Voice {
  @override
  Future<bool> available() async => false;

  @override
  Future<void> say(String text) async {}

  @override
  Future<void> silence() async {}

  @override
  Future<void> dispose() async {}
}

/// _short : A line of text, cut down for a log.
String _short(String text) =>
    text.length <= 48 ? text : '${text.substring(0, 48)}…';
