import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/api/client.dart';
import 'package:personal_assistant_client/design/design.dart';

/// host : The button under the app's theme, which its colours come from.
Widget host(Future<AnswerTimeline> Function(String) load) => MaterialApp(
  theme: AppTheme.dark,
  home: Scaffold(
    body: SingleChildScrollView(
      child: AppTimelineButton(chatId: 'chat_1', load: load),
    ),
  ),
);

/// made : A timeline with one of everything in it.
AnswerTimeline made() => AnswerTimeline.fromJson({
  'chat_id': 'chat_1',
  'status': 'completed',
  'took_ms': 4202,
  'complete': true,
  'steps': [
    {'kind': 'asked', 'offset_ms': 0, 'text': 'what did the builder charge'},
    {
      'kind': 'recalled',
      'took_ms': 37,
      'recalled': {
        'always': [
          {'id': 'mem_a', 'text': 'Home: Lives in Chennai.'},
        ],
        'notes': [
          {
            'id': 'mem_b',
            'text': 'Roof quote: forty thousand.',
            'score': 0.588,
          },
        ],
        'exchanges': [
          {
            'message_id': 'msg_1',
            'conversation_id': 'conv_1',
            'text': 'They said: the terrace',
            'score': 0.484,
          },
        ],
      },
    },
    {
      'kind': 'tool_call',
      'offset_ms': 1976,
      'name': 'memory_search',
      'arguments': '{"about":"the roof"}',
    },
    {
      'kind': 'tool_result',
      'offset_ms': 2004,
      'name': 'memory_search',
      'outcome': 'ok',
      'content': 'mem_b  Roof quote: forty thousand.',
      'took_ms': 25,
    },
    {'kind': 'answered', 'offset_ms': 4202, 'text': 'Forty thousand rupees.'},
  ],
});

void main() {
  // Closed by default. The answer is what somebody came for; how it was
  // arrived at matters only when it looks wrong.
  testWidgets('nothing is shown until it is opened', (tester) async {
    var asked = 0;
    await tester.pumpWidget(
      host((_) async {
        asked++;
        return made();
      }),
    );

    expect(find.text('How this answer was made'), findsNothing);
    expect(asked, 0, reason: 'it fetched a timeline nobody asked to see');
  });

  testWidgets('opening it shows every step in order', (tester) async {
    await tester.pumpWidget(host((_) async => made()));

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    expect(find.text('How this answer was made'), findsOneWidget);
    expect(find.text('You asked'), findsOneWidget);
    expect(find.text('Recalled'), findsOneWidget);
    expect(find.text('Called memory_search'), findsOneWidget);
    expect(find.text('memory_search ok'), findsOneWidget);
    expect(find.text('Answered'), findsOneWidget);
  });

  // The scores are the point: they tell "it never found it" from "it found
  // it and ignored it", which need different fixes.
  testWidgets('what was recalled is shown with its score', (tester) async {
    await tester.pumpWidget(host((_) async => made()));

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    expect(find.text('0.588'), findsOneWidget);
    expect(find.text('0.484'), findsOneWidget);
    expect(find.text('always'), findsOneWidget);
    expect(find.text('Roof quote: forty thousand.'), findsOneWidget);
  });

  testWidgets('durations are shown in a readable unit', (tester) async {
    await tester.pumpWidget(host((_) async => made()));

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    // Twice, and both are right: the whole answer took 4.2s, and the step
    // that produced it sits at 4.2s down the side.
    expect(find.text('4.2s'), findsNWidgets(2));
    expect(find.text('0ms'), findsOneWidget, reason: 'the question');
    expect(find.text('· 25ms'), findsOneWidget, reason: 'the tool');
    // Also twice: the call at 1976ms and its result at 2004ms both land in
    // the same second, which is what a tenth-of-a-second scale means.
    expect(find.text('2.0s'), findsNWidgets(2));
  });

  // An answer from before the server recorded this says so, rather than
  // showing an empty timeline as though nothing had happened.
  testWidgets('an answer with no record says so', (tester) async {
    await tester.pumpWidget(
      host(
        (_) async => AnswerTimeline.fromJson({
          'chat_id': 'chat_1',
          'complete': false,
          'steps': <dynamic>[],
        }),
      ),
    );

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    expect(
      find.textContaining('before the server kept a record'),
      findsOneWidget,
    );
  });

  // This is the screen somebody opened because something already looked
  // wrong, so a failure to load it is said rather than swallowed.
  testWidgets('a failure to load is shown', (tester) async {
    await tester.pumpWidget(host((_) async => throw Exception('no route')));

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    expect(find.textContaining('no route'), findsOneWidget);
  });

  testWidgets('it is fetched once however often it is opened', (tester) async {
    var asked = 0;
    await tester.pumpWidget(
      host((_) async {
        asked++;
        return made();
      }),
    );

    for (var i = 0; i < 3; i++) {
      await tester.tap(find.byType(InkWell).first);
      await tester.pumpAndSettle();
    }

    expect(asked, 1, reason: 'it refetched something that cannot change');
  });

  // Matching by wording rather than meaning is a degraded state, and the
  // panel is where somebody would find out it is happening.
  testWidgets('a word-only match is called out', (tester) async {
    await tester.pumpWidget(
      host(
        (_) async => AnswerTimeline.fromJson({
          'chat_id': 'chat_1',
          'complete': true,
          'steps': [
            {
              'kind': 'recalled',
              'by_words': true,
              'recalled': {
                'notes': [
                  {'id': 'mem_b', 'text': 'Roof quote', 'score': 0.5},
                ],
              },
            },
          ],
        }),
      ),
    );

    await tester.tap(find.byType(InkWell));
    await tester.pumpAndSettle();

    expect(find.textContaining('Matched by wording'), findsOneWidget);
  });
}
