/// Hearing what is said.
library;

import 'package:flutter/foundation.dart';

/// Heard : What the recogniser currently believes was said.
@immutable
class Heard {
  const Heard(this.text, {required this.settled});

  final String text;

  /// settled : Whether the recogniser has finished and will not revise
  /// this. A partial result is worth showing so the user can see they are
  /// being heard, but only a settled one should be sent.
  final bool settled;

  @override
  String toString() => 'Heard("$text", settled: $settled)';
}

/// ListeningState : What the microphone is doing.
enum ListeningState {
  /// off : Not listening.
  off,

  /// starting : Permission and the engine are being arranged.
  starting,

  /// listening : Hearing.
  listening,

  /// unavailable : This device cannot listen, or permission was refused.
  unavailable,
}

/// Ears : The engine that turns sound into text.
abstract interface class Ears {
  /// prepare : Asks for permission and readies the engine.
  ///
  /// Returns null when listening is possible, and otherwise a sentence the
  /// user can act on. A sentence rather than a flag because the reasons are
  /// not the same and neither is what to do about them: a refused microphone
  /// is a permission to grant, a model that would not download is a network
  /// to check, and telling someone the wrong one sends them somewhere there
  /// is nothing to fix.
  Future<String?> prepare();

  /// endpointsItself : Whether a settled result means the speaker has
  /// actually stopped.
  ///
  /// Whisper is told when to stop by code in this repository, which has
  /// measured the silence first, so settled means settled. Android's
  /// recogniser and a browser's both call a result final the moment they
  /// think an utterance is complete, and they think it early — a filler, or
  /// a second of thought, and the sentence is declared over. What follows a
  /// result from those has to be waited on.
  bool get endpointsItself;

  /// settleBeforeReopen : How long to leave this engine alone between one
  /// turn and waiting for the next.
  ///
  /// Engine-specific knowledge, so the engine owns it. Android's recogniser
  /// refuses a session started too soon after the last and answers
  /// ERROR_RECOGNIZER_BUSY; a browser's needs almost nothing; one that never
  /// stops needs none at all.
  Duration get settleBeforeReopen;

  /// preparing : How far one-time setup has got, from zero to one, or null
  /// when nothing is being prepared.
  ///
  /// Listening can mean fetching half a gigabyte of weights the first time.
  /// Without somewhere to say so, that is several minutes of an open
  /// microphone that hears nothing, which looks exactly like a fault.
  ValueListenable<double?> get preparing;

  /// listen : Starts listening, reporting what it hears as it changes.
  ///
  /// [onResult] is called repeatedly with partial text and finally with a
  /// settled one. [onDone] fires when the engine stops on its own, which it
  /// does after a pause in speech.
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  });

  /// stop : Stops listening and keeps what was heard.
  Future<void> stop();

  /// cancel : Stops listening and throws away what was heard.
  Future<void> cancel();

  /// dispose : Releases the engine.
  Future<void> dispose();
}

/// DeafEars : Ears that hear nothing, for a device without a microphone and
/// for tests.
class DeafEars implements Ears {
  @override
  final ValueListenable<double?> preparing = ValueNotifier<double?>(null);

  @override
  Duration get settleBeforeReopen => Duration.zero;

  @override
  bool get endpointsItself => false;

  @override
  Future<String?> prepare() async => 'This device cannot listen.';

  @override
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    onError('This device cannot listen.');
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> cancel() async {}

  @override
  Future<void> dispose() async {}
}
