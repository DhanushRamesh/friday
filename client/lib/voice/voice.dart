/// Speaking and listening.
///
/// The interfaces — [Voice] and [Ears] — are separate from the plugins that
/// implement them so that the logic above them, chiefly the speech queue,
/// can be tested without a synthesiser or a microphone, and so that Android
/// can later use something the browser cannot offer.
library;

export 'language.dart';
export 'listener.dart';
export 'platform_ears.dart';
export 'plugin_ears.dart';
export 'platform_voice.dart';
export 'plugin_voice.dart';
export 'speaker.dart';
export 'wake_rule.dart';
export 'staying_awake.dart';
export 'platform_awake.dart';
export 'hushing_the_tone.dart';
export 'platform_hush.dart';
