import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/voice/voice.dart';

void main() {
  const rule = NameSpoken.called('friday');

  // The name stays in what is sent. Taking it off was tried twice and
  // ruled against twice — "dont strip out friday word". Every rule for
  // deciding what counts as the question is wrong somewhere: "hello FRIDAY"
  // loses its greeting, the name at the end means looking backwards, a name
  // in the middle means guessing which side the question is on. The model
  // reads "FRIDAY, what is a mutex" perfectly well.
  group('addressed to FRIDAY', () {
    test('the whole utterance is sent, name and all', () {
      for (final said in [
        'friday what is a mutex',
        'hey friday what is a mutex',
        'what is a mutex friday',
        'hello friday',
        'friday',
        'Friday, what is a mutex?',
        'friday no friday what is a mutex',
      ]) {
        final got = rule.check(said);
        expect(got.forFriday, isTrue, reason: said);
        expect(got.question, said, reason: said);
      }
    });

    test('surrounding space is trimmed', () {
      expect(rule.check('  friday hello  ').question, 'friday hello');
    });
  });

  // The name is a setting, not a fact about the code.
  group('a name of one\'s own', () {
    const computer = NameSpoken.called('computer');

    test('answers to the configured name', () {
      expect(computer.check('computer what is a mutex').forFriday, isTrue);
      expect(
        computer.check('computer what is a mutex').question,
        'computer what is a mutex',
      );
    });

    test('and not to the old one', () {
      expect(computer.check('friday what is a mutex').forFriday, isFalse);
    });

    // The misheard list was collected by listening to a recogniser get
    // "friday" wrong. A different name has no such list and gets the loose
    // match alone, which must still not answer to a plain word.
    test('a configured name keeps the loose match but not another name\'s '
        'mishearings', () {
      expect(
        computer.check('computers are fast').forFriday,
        isTrue,
        reason: 'one edit away is still the name',
      );
      expect(
        computer.check('freddy what is a mutex').forFriday,
        isFalse,
        reason: "freddy is on friday's list, not this one",
      );
    });

    test('the default is FRIDAY when nothing is configured', () {
      expect(wakeName(), 'friday');
      expect(NameSpoken().check('friday hello').forFriday, isTrue);
    });
  });

  group('not addressed to FRIDAY', () {
    // The whole point: the room talking must not reach the model. Every
    // stray sentence would otherwise be a prompt, and a bill.
    test('ordinary conversation', () {
      for (final said in [
        'did you watch the match last night',
        'what is a mutex',
        'tell me when you are ready',
        'fried rice please',
        'fresh air',
        '',
        '   ',
      ]) {
        expect(rule.check(said).forFriday, isFalse, reason: said);
      }
    });

    // The accepted cost of matching the name wherever it is said.
    test('the name said about someone else is answered anyway', () {
      expect(rule.check('I will do it on Friday').forFriday, isTrue);
      expect(rule.check('fridays are busy').forFriday, isTrue);
    });
  });

  // Holding the microphone open by hand is itself how the user says who
  // they are talking to, so nothing has to be named.
  test('with the button, everything is addressed', () {
    const held = AlwaysAddressed();
    expect(held.check('what is a mutex').forFriday, isTrue);
    expect(held.check('what is a mutex').question, 'what is a mutex');
  });

  test('a different name can be answered to', () {
    const other = NameSpoken.called('computer');
    expect(other.check('computer what is a mutex').forFriday, isTrue);
    expect(other.check('friday what is a mutex').forFriday, isFalse);
  });

  // While FRIDAY is talking its own voice comes back through the
  // microphone and reaches the transcript first. That echo is the only
  // thing dropped anywhere.
  group('interrupting an answer', () {
    test('takes from the name onwards, dropping the echo before it', () {
      expect(
        rule.interruptionIn(
          'a mutex is a lock friday no tell me about channels',
        ),
        'friday no tell me about channels',
      );
    });

    test('takes the last mention, not the first', () {
      expect(rule.interruptionIn('friday said friday stop'), 'friday stop');
    });

    test('the name at the end leaves just the name', () {
      expect(rule.interruptionIn('a mutex is a lock friday'), 'friday');
    });

    test('no name means no interruption', () {
      expect(rule.interruptionIn('a mutex is a lock that protects'), isNull);
      expect(rule.interruptionIn(''), isNull);
    });

    // If the answer itself says the name, an interruption cannot be told
    // from the echo.
    test('reports whether text contains the name at all', () {
      expect(rule.says('I will remind you on friday'), isTrue);
      expect(rule.says('a mutex is a lock'), isFalse);
      expect(rule.says('fridays are busy'), isTrue);
    });
  });

  // Spoken normally rather than announced, the name comes back from the
  // recogniser wrong often enough that an exact match means saying it
  // again, louder, until it lands.
  group('the recogniser mishearing the name', () {
    test('accepts a near miss', () {
      for (final said in [
        'frida what is a mutex',
        'fridayy what is a mutex',
        'friay what is a mutex',
        'fridays what is a mutex',
        'freddy what is a mutex',
      ]) {
        expect(rule.check(said).forFriday, isTrue, reason: said);
      }
    });

    // Two edits would let in ordinary words somebody might say while
    // arranging a meeting.
    test('does not accept another word entirely', () {
      for (final said in [
        'sunday what is a mutex',
        'monday what is a mutex',
        'fridge what is a mutex',
        'holiday what is a mutex',
      ]) {
        expect(rule.check(said).forFriday, isFalse, reason: said);
      }
    });

    // A single edit covers too much of the language at three or four
    // letters.
    test('a short name is matched exactly', () {
      const short = NameSpoken.called('fry');
      expect(short.check('fry what is a mutex').forFriday, isTrue);
      expect(short.check('dry what is a mutex').forFriday, isFalse);
    });
  });
}
