/// What language FRIDAY listens in, and what it sounds like.
library;

import 'dart:ui' show PlatformDispatcher;

/// _hearing : Set with `--dart-define=FRIDAY_LANG=en-IN`, or
/// `make ui FRIDAY_LANG=en-IN`.
const String _hearing = String.fromEnvironment('FRIDAY_LANG');

/// _speaking : Set with `--dart-define=FRIDAY_VOICE=en-GB`, or
/// `make ui FRIDAY_VOICE=en-GB`.
const String _speaking = String.fromEnvironment('FRIDAY_VOICE');

/// _defaultVoice : What FRIDAY sounds like unless told otherwise.
const String _defaultVoice = 'en-US';

/// _fallback : Used when the device reports nothing usable.
const String _fallback = 'en-US';

/// hearingLanguage : The language and accent to recognise speech as.
///
/// This is the single biggest lever on how accurately words are captured,
/// and it is not a detail. The recogniser picks an acoustic model from
/// this tag; given the wrong one it mishears steadily, and speaking more
/// clearly does not help — `en-GB` was hard-coded at first, which
/// penalised anyone who does not speak that way.
///
/// It should be how the *user* speaks, which is not necessarily the
/// device's locale, hence the override.
String hearingLanguage() {
  if (_hearing.isNotEmpty) return _hearing;

  final locale = PlatformDispatcher.instance.locale;
  final language = locale.languageCode;
  if (language.isEmpty || language == 'und') return _fallback;

  final country = locale.countryCode;
  return (country == null || country.isEmpty) ? language : '$language-$country';
}

/// speakingLanguage : The voice FRIDAY answers in.
///
/// Deliberately separate from [hearingLanguage]. What someone speaks and
/// what they want read back to them are different choices: hearing has to
/// match the speaker or accuracy suffers, while the voice is a preference.
String speakingLanguage() => _speaking.isNotEmpty ? _speaking : _defaultVoice;
