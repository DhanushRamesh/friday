import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/voice/transcript.dart';

void main() {
  group('spoken words', () {
    // The one that got out. It reached the screen as though it were an
    // answer, and would have been sent to the model as a question.
    test('drops the blank-audio annotation entirely', () {
      expect(spokenWords('[BLANK_AUDIO]'), isEmpty);
    });

    test('drops an annotation from the middle of a sentence', () {
      expect(
        spokenWords('what is a mutex [BLANK_AUDIO] in go'),
        'what is a mutex in go',
      );
    });

    test('drops the parenthesised kind too', () {
      expect(spokenWords('(wind blowing) hello friday'), 'hello friday');
    });

    test('leaves ordinary speech alone', () {
      const said = 'tell me about Iron Man';
      expect(spokenWords(said), said);
    });

    test('collapses the gap an annotation leaves behind', () {
      expect(spokenWords('hello   [MUSIC]   friday'), 'hello friday');
    });

    test('an empty transcript stays empty', () {
      expect(spokenWords(''), isEmpty);
      expect(spokenWords('   '), isEmpty);
    });
  });
}
