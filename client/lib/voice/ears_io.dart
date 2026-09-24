/// Listening on a phone or a desktop.
library;

import 'dart:io';

import 'listener.dart';
import 'plugin_ears.dart';
import 'sherpa_ears.dart';
import 'whisper_ears.dart';

/// _chosen : Which recogniser Android uses — native, whisper or sherpa —
/// set at build time with `--dart-define=FRIDAY_EARS=sherpa`.
///
/// The platform recogniser, and this was measured rather than reasoned.
/// Whisper was tried twice on the owner's phone and lost twice: *"gogle
/// speech reciognozer was better..whiser is slow and not recirniziubg the
/// words ...WER is large"*.
///
/// small.en decoded slower than speech. base.en kept up and misheard, which
/// is unsurprising against a Google model trained for en-IN in particular —
/// base is Whisper's weak tier and English-general. The reasoning that sent
/// us to Whisper came from complaints about the *browser's* recogniser, a
/// different engine entirely, and Android's own was never measured until
/// after all of it was built.
///
/// The code stays. It is the right answer the day a larger model runs on
/// the server, where decode speed is not a phone's problem, and it is the
/// only path that owns the audio — which is what a microphone that never
/// stops, echo cancellation, and any kind of voice matching all need.
const String _chosen = String.fromEnvironment(
  'FRIDAY_EARS',
  defaultValue: 'native',
);

/// createEars : The recogniser for this platform.
///
/// Only Android has a choice. Everything else has one recogniser and uses
/// it.
Ears createEars({String language = 'en-US'}) {
  if (Platform.isAndroid) {
    switch (_chosen) {
      case 'whisper':
        return WhisperEars(language: language);
      case 'sherpa':
        return SherpaEars(language: language);
    }
  }
  return PluginEars(language: language);
}
