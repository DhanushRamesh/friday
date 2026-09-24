/// Cleaning up what the recogniser produces.
library;

/// _annotation : A bracketed aside, which is how Whisper writes what it
/// heard that was not speech: [BLANK_AUDIO], (wind blowing), [MUSIC].
final RegExp _annotation = RegExp(r'\[[^\]]*\]|\([^)]*\)');

/// _runsOfSpace : Whatever whitespace removing an annotation left behind.
final RegExp _runsOfSpace = RegExp(r'\s+');

/// spokenWords : What was actually said, with the recogniser's own
/// annotations taken out.
///
/// Whisper describes non-speech rather than ignoring it, and asking it to
/// suppress those tokens does not catch every one — `[BLANK_AUDIO]` reached
/// the screen as though it were an answer, and would have been sent to the
/// model as a question.
///
/// Bracketed text is removed wholesale. Someone could in principle dictate a
/// parenthesis and lose it, but nobody speaks that way, and the alternative
/// is sending the word BLANK_AUDIO to an assistant and having it try.
String spokenWords(String raw) =>
    raw.replaceAll(_annotation, ' ').replaceAll(_runsOfSpace, ' ').trim();
