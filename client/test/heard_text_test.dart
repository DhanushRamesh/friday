import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/state/heard_text.dart';

void main() {
  group('joining what was heard', () {
    test('puts two different pieces together', () {
      expect(
        joinHeard('hello friday', 'how are you'),
        'hello friday how are you',
      );
    });

    // The staircase. Android reports the whole utterance on every result,
    // and a stretch can be recorded as ended while the same session keeps
    // reporting — so each report arrived carrying the one before it.
    test(
      'replaces rather than repeats when the segment already contains it',
      () {
        expect(
          joinHeard('Friday can you', 'Friday can you go to sleep'),
          'Friday can you go to sleep',
        );
      },
    );

    test('survives the whole climb', () {
      var heard = '';
      for (final report in [
        'Friday',
        'Friday can',
        'Friday can you',
        'Friday can you go',
        'Friday can you go to sleep',
      ]) {
        heard = joinHeard(heard, report);
      }
      expect(heard, 'Friday can you go to sleep');
    });

    // A recogniser revises its own punctuation as it goes, so the repeat is
    // not always character for character.
    test('sees through changed punctuation and case', () {
      expect(
        joinHeard('friday can', 'Friday, can you help?'),
        'Friday, can you help?',
      );
    });

    test('an empty side leaves the other alone', () {
      expect(joinHeard('', 'hello'), 'hello');
      expect(joinHeard('hello', ''), 'hello');
      expect(joinHeard('', ''), isEmpty);
    });

    // Two genuinely different sentences must still both survive, even when
    // the second happens to begin with a word from the first.
    test('keeps both when the second only shares a word', () {
      expect(
        joinHeard('what is a mutex', 'is it fast'),
        'what is a mutex is it fast',
      );
    });
  });
}
