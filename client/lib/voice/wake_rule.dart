/// Deciding whether something said was meant for FRIDAY.
library;

/// Addressed : What a heard utterance amounts to.
class Addressed {
  const Addressed.no() : forFriday = false, question = '';
  const Addressed.yes(this.question) : forFriday = true;

  /// forFriday : Whether the name was said, so this was meant for FRIDAY
  /// rather than being the room talking.
  final bool forFriday;

  /// question : What to send. The name is left in it.
  ///
  /// Taking it off was tried twice and ruled against twice — *"dont strip
  /// out friday word"*. Every rule for deciding what counts as the question
  /// is wrong somewhere: "hello FRIDAY" loses its greeting, the name at the
  /// end means looking backwards, and a name in the middle means guessing
  /// which side the question is on. The model reads "FRIDAY, what is a
  /// mutex" perfectly well, so there is nothing to buy with the guessing.
  final String question;

  @override
  String toString() =>
      forFriday ? 'Addressed("$question")' : 'Addressed(not for FRIDAY)';
}

/// WakeRule : Decides whether a heard utterance was addressed to FRIDAY.
///
/// This is the whole of always-awake. A separate wake-word engine would be
/// cheaper in power and better at ignoring the room, but it needs a model,
/// an account and a key; the name at the front of the sentence needs
/// nothing and is said in one breath with the question, which a detector
/// that has to hand the microphone over cannot do.
abstract interface class WakeRule {
  /// check : Reads an utterance and says whether to act on it.
  Addressed check(String heard);

  /// interruptionIn : What was said from the name onwards, when the name
  /// appears anywhere at all — or null if it does not.
  ///
  /// Used only while FRIDAY is talking. On a laptop its own voice comes
  /// back through the microphone, so the transcript begins with whatever
  /// FRIDAY is saying and the name lands in the middle of it. Requiring
  /// the name first, which is right the rest of the time, would mean the
  /// user could never interrupt.
  String? interruptionIn(String heard);

  /// says : Whether this text contains the name.
  ///
  /// Asked of what FRIDAY is currently saying: if the answer itself
  /// contains the name, an interruption cannot be told from the echo, so
  /// none is claimed.
  bool says(String text);
}

/// wakeName : What FRIDAY answers to.
///
/// Set with `--dart-define=FRIDAY_NAME=jarvis`, or `make apk
/// FRIDAY_NAME=jarvis`. Lower case, one word: it is matched word by word
/// against the transcript, so a name with a space in it would never match.
///
/// A short name is matched exactly and a long one loosely, because at three
/// or four letters a single misheard character covers too much of the
/// language to be safe.
String wakeName() {
  const configured = String.fromEnvironment('FRIDAY_NAME');
  final name = configured.trim().toLowerCase();
  return name.isEmpty ? 'friday' : name;
}

/// _misheard : What a recogniser commonly returns instead of "friday".
///
/// Spoken normally rather than announced, the name comes back wrong often
/// enough that an exact match means saying it again, louder, until it
/// lands — which is not what talking to an assistant should be like.
///
/// Only FRIDAY has a list, because these were collected by listening to a
/// recogniser get it wrong. A different name gets the loose match and
/// nothing else until somebody does the same work for it.
const Map<String, List<String>> _misheard = {
  'friday': ['fridays', 'freddie', 'freddy', 'frisbee', 'friyay', 'privacy'],
};

/// _nearness : How many single-character changes still count as the name.
///
/// One. Two would let "sunday" and "monday" through, which are ordinary
/// words in a room where somebody is arranging a meeting.
const int _nearness = 1;

/// _within : Whether two words differ by at most [limit] edits.
///
/// Levenshtein, stopped early: the words are short and this runs on every
/// partial result, of which there are many a second.
bool _within(String a, String b, int limit) {
  if ((a.length - b.length).abs() > limit) return false;
  if (a == b) return true;

  var previous = List<int>.generate(b.length + 1, (i) => i);
  for (var i = 1; i <= a.length; i++) {
    final current = <int>[i, ...List<int>.filled(b.length, 0)];
    var best = i;
    for (var j = 1; j <= b.length; j++) {
      final cost = a[i - 1] == b[j - 1] ? 0 : 1;
      current[j] = [
        current[j - 1] + 1,
        previous[j] + 1,
        previous[j - 1] + cost,
      ].reduce((x, y) => x < y ? x : y);
      if (current[j] < best) best = current[j];
    }
    // Nothing on this row is close enough, so nothing below it can be.
    if (best > limit) return false;
    previous = current;
  }
  return previous[b.length] <= limit;
}

/// NameSpoken : Addressed when FRIDAY's name is said at all.
///
/// Anywhere in the utterance, not only at the front. Requiring it first
/// reads better on paper and rejects more of the room, but in use the
/// recogniser drops or mangles the first word often enough that a
/// question has to be repeated, and FRIDAY's own voice coming back gets
/// to the front of the transcript before the user does.
///
/// The cost, accepted: "I'll do it on Friday" said to somebody else in
/// the room will be answered.
class NameSpoken implements WakeRule {
  NameSpoken({String? name}) : name = name ?? wakeName();

  /// NameSpoken.called : A rule for a name given outright, for tests and
  /// for anywhere the build-time setting is not what is wanted.
  const NameSpoken.called(this.name);

  /// name : What FRIDAY answers to, lower case.
  final String name;

  @override
  Addressed check(String heard) {
    // Whitespace only. The punctuation belongs to the sentence, and the
    // name is matched with it removed word by word rather than by
    // trimming the whole utterance.
    final text = heard.trim();
    if (text.isEmpty) return const Addressed.no();

    if (!text.split(RegExp(r'\s+')).any(_isName)) return const Addressed.no();

    // The whole utterance, name and all. Nothing is taken off.
    return Addressed.yes(text);
  }

  @override
  String? interruptionIn(String heard) {
    final words = heard.trim().split(RegExp(r'\s+'));
    final at = words.lastIndexWhere(_isName);
    if (at < 0) return null;

    // From the name onwards, the name included. Everything before it is
    // FRIDAY's own answer coming back through the microphone, and that
    // is the only thing dropped anywhere.
    return words.sublist(at).join(' ').trim();
  }

  @override
  bool says(String text) => text.split(RegExp(r'\s+')).any(_isName);

  /// _isName : Whether a word is FRIDAY's name, allowing for the
  /// recogniser hearing it imperfectly.
  ///
  /// Short words are matched exactly: at three or four letters a single
  /// edit covers too much of the language to be safe.
  bool _isName(String word) {
    final bare = _bare(word);
    if (bare == name) return true;
    if (_misheard[name]?.contains(bare) ?? false) return true;
    if (name.length < 5) return false;
    return _within(bare, name, _nearness);
  }

  /// _bare : A word with its punctuation and case removed, so that
  /// "Friday," and "friday" are the same word.
  String _bare(String word) =>
      word.toLowerCase().replaceAll(RegExp(r'[^a-z]'), '');
}

/// AlwaysAddressed : Everything heard is meant for FRIDAY.
///
/// What holding the microphone open by hand means: pressing the button is
/// itself the way of saying who you are talking to.
class AlwaysAddressed implements WakeRule {
  const AlwaysAddressed();

  @override
  Addressed check(String heard) => Addressed.yes(heard.trim());

  @override
  String? interruptionIn(String heard) => heard.trim();

  @override
  bool says(String text) => false;
}
