/// Silencing the recognition tone, where there is none.
library;

import 'hushing_the_tone.dart';

/// createHushingTheTone : Nothing here makes a noise.
HushingTheTone createHushingTheTone() => const NoToneToHush();
