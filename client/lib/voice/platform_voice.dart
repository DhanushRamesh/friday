/// Choosing how to speak.
///
/// The browser is driven directly and the phone through flutter_tts. The
/// plugin's web implementation reported no voices on a browser that had
/// nineteen, and the web path needs to choose a voice and know exactly when
/// an utterance ends, so it is owned rather than delegated.
library;

import 'voice_stub.dart'
    if (dart.library.io) 'voice_io.dart'
    if (dart.library.js_interop) 'voice_web.dart'
    as platform;

import 'language.dart';
import 'speaker.dart';

/// defaultVoice : The synthesiser for the platform this is running on.
Voice defaultVoice() => platform.createVoice(language: speakingLanguage());
