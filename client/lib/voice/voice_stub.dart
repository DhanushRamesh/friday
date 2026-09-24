/// Speaking where there is no known way to.
library;

import 'speaker.dart';

/// createVoice : Something that says nothing, rather than a build failure.
Voice createVoice({String language = 'en-US', double rate = 0.52}) =>
    SilentVoice();
