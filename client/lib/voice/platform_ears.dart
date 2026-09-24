/// Choosing how to listen.
///
/// The browser is driven directly and the phone through speech_to_text.
/// The plugin's web implementation keeps its own idea of whether it is
/// listening, and it drifts from the browser's: it decides it has
/// stopped, so `cancel` does nothing, while SpeechRecognition is still
/// running and refuses every `start`. Holding the object ourselves is the
/// only way to be sure it has actually stopped.
library;

import 'ears_stub.dart'
    if (dart.library.io) 'ears_io.dart'
    if (dart.library.js_interop) 'ears_web.dart'
    as platform;

import 'language.dart';
import 'listener.dart';

/// defaultEars : The recogniser for the platform this is running on.
Ears defaultEars() => platform.createEars(language: hearingLanguage());
