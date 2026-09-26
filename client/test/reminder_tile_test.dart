import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/api/client.dart';

/// A reminder parses what the server sends, including the parts a listing
/// shows.
void main() {
  test('a reminder is parsed as the server sends it', () {
    final r = Reminder.fromJson({
      'id': 'rem_01M3D477HXQ4YNQX7BNXJZZCV0',
      'title': 'Wake up',
      'say': 'It is seven o\'clock.',
      'due_at': '2026-09-28T01:30:00Z',
      'repeats': 'weekdays',
      'scope': 'user',
      'status': 'pending',
      'fires': 2,
    });

    expect(r.title, 'Wake up');
    expect(r.say, "It is seven o'clock.");
    expect(r.repeating, isTrue);
    expect(r.pending, isTrue);
    expect(r.fires, 2);
    expect(r.dueAt.isUtc, isFalse, reason: 'shown in the reader\'s own time');
  });

  // A one-shot is not marked as repeating, which decides its icon and
  // whether the listing says how often.
  test('a one-shot does not report itself repeating', () {
    final r = Reminder.fromJson({
      'id': 'rem_1',
      'title': 'Timer',
      'say': 'Up.',
      'due_at': '2026-09-26T12:20:00Z',
    });

    expect(r.repeating, isFalse);
    expect(r.repeats, '');
    expect(r.scope, 'user', reason: 'the default follows the person');
  });

  // Anything that already happened is not pending, so it can be told from
  // what is still coming.
  test('something finished is not pending', () {
    for (final status in ['done', 'missed', 'cancelled']) {
      final r = Reminder.fromJson({
        'id': 'rem_1',
        'title': 'Timer',
        'say': 'Up.',
        'due_at': '2026-09-26T12:20:00Z',
        'status': status,
      });
      expect(r.pending, isFalse, reason: status);
    }
  });

  // A body the server has not sent yet must not crash the list.
  testWidgets('a half-filled reminder still renders', (tester) async {
    final r = Reminder.fromJson({'id': 'rem_1'});

    await tester.pumpWidget(MaterialApp(home: Scaffold(body: Text(r.title))));
    expect(r.title, '');
    expect(r.say, '');
  });
}
