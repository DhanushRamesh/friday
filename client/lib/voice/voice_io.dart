/// Speaking on a phone or a desktop, through the platform's own engine.
library;

import 'plugin_voice.dart';
import 'speaker.dart';

/// createVoice : The platform synthesiser, via flutter_tts.
Voice createVoice({String language = 'en-US', double rate = 0.52}) =>
    PluginVoice(language: language, rate: rate);
