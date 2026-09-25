/// The app's design system.
///
/// Two layers. `tokens.dart` holds the values — colours, gaps, radii, type,
/// motion — reached through the theme as `context.colors` and `context.text`.
/// Everything else is a component, prefixed `App`, built only from those
/// values.
///
/// A screen should not need `Container`, a raw `Color`, or a number for a
/// gap. If it does, the missing thing belongs here.
library;

export 'components/banner.dart';
export 'components/button.dart';
export 'components/composer.dart';
export 'components/session_tile.dart';
export 'components/spinner.dart';
export 'components/status.dart';
export 'components/surface.dart';
export 'components/text_field.dart';
export 'components/turn.dart';
export 'theme.dart';
export 'tokens.dart';
