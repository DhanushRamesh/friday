/// Joining up what successive stretches of recognition heard.
library;

/// joinHeard : What the turn has heard, given what earlier stretches kept
/// and what the current one is saying.
///
/// Usually these are different pieces of one sentence and are put together.
/// Sometimes they are not: Android's recogniser reports the whole utterance
/// so far on every result, and a stretch can be recorded as ended while that
/// same session carries on reporting. Joined blindly, each report is added to
/// the last one it already contained, and a question climbs a staircase —
///
///     Friday Friday can Friday can you Friday can you go to sleep
///
/// So a segment that already begins with everything kept replaces it rather
/// than being added to it.
String joinHeard(String committed, String segment) {
  final kept = committed.trim();
  final saying = segment.trim();

  if (kept.isEmpty) return saying;
  if (saying.isEmpty) return kept;
  if (_startsWith(saying, kept)) return saying;

  return '$kept $saying';
}

/// _startsWith : Whether the longer text opens with the shorter one, ignoring
/// case and how the words are spaced.
///
/// Compared loosely because a recogniser revises its own punctuation and
/// capitalisation as it goes: "friday can" becomes "Friday, can".
bool _startsWith(String text, String opening) {
  final a = _plain(text);
  final b = _plain(opening);
  return b.isNotEmpty && a.startsWith(b);
}

final RegExp _notWords = RegExp(r"[^a-z0-9' ]");
final RegExp _spaces = RegExp(r'\s+');

String _plain(String text) => text
    .toLowerCase()
    .replaceAll(_notWords, ' ')
    .replaceAll(_spaces, ' ')
    .trim();
