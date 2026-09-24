/// Choosing what silences the recognition tone.
library;

import 'hushing_the_tone_stub.dart'
    if (dart.library.io) 'hushing_the_tone_io.dart'
    as platform;

import 'hushing_the_tone.dart';

/// defaultHushingTheTone : What silences the tone on this platform.
HushingTheTone defaultHushingTheTone() => platform.createHushingTheTone();
