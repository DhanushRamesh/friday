import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/friday/friday.dart';
import 'package:friday_client/state/conversation.dart';

import 'support.dart';

/// settle : Lets pending microchats and zero-delay timers run, which is how
/// the scripted stream delivers its chunks.
Future<void> settle([int rounds = 12]) async {
  for (var i = 0; i < rounds; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

/// conversationOn : A conversation over a fake server and a scripted stream.
Future<(Conversation, FakeServer, ScriptedByteSource)> conversationOn(
  WidgetTester? _,
  List<StreamAnswer> answers,
) async {
  final server = FakeServer();
  final bytes = ScriptedByteSource(answers);
  final api = loggedIn(server, bytes: bytes);
  await authorise(api);
  final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
  return (conversation, server, bytes);
}

void main() {
  group('sending', () {
    // The prompt must be on screen before the request finishes: the user has
    // already committed to it, and waiting a round trip to redraw makes the
    // interface feel slower than the network is.
    test('shows the prompt before the server has accepted it', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('final', seq: 1, text: 'the answer')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);

      final sending = conversation.send('what is Go?');

      expect(conversation.exchanges.single.prompt, 'what is Go?');
      expect(conversation.exchanges.single.sending, isTrue);
      expect(conversation.exchanges.single.chatId, isNull);

      await sending;
      await settle();

      expect(conversation.exchanges.single.sending, isFalse);
      expect(conversation.exchanges.single.chatId, 'chat_01DDD');
    });

    test('builds up the updates and then the answer', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(
          chunks: [
            sse('update', seq: 1, text: 'Let me look.'),
            sse('update', seq: 2, text: 'Still working.'),
            sse('final', seq: 3, text: 'Go is a language.'),
          ],
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await conversation.send('what is Go?');
      await settle();

      final exchange = conversation.exchanges.single;
      expect(exchange.updates, ['Let me look.', 'Still working.']);
      expect(exchange.answer, 'Go is a language.');
      expect(exchange.status, ChatStatus.completed);
      expect(exchange.isRunning, isFalse);
      expect(conversation.isIdle, isTrue);
    });

    test('reports a failed chat with the reason', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('error', text: 'I could not reach GitLab.')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await conversation.send('check GitLab');
      await settle();

      final exchange = conversation.exchanges.single;
      expect(exchange.failure, 'I could not reach GitLab.');
      expect(exchange.status, ChatStatus.failed);
    });

    // A prompt that never reached the server is not a chat that failed: it
    // can be sent again exactly as typed.
    test('marks a prompt that never reached the server as retryable', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('final', seq: 1, text: 'ok')]),
      ]);
      server.onRequest('POST /v1/chats', (_) => throw const Dropped());

      await conversation.send('what is Go?');

      final exchange = conversation.exchanges.single;
      expect(exchange.rejected, isTrue);
      expect(exchange.isRunning, isFalse);
      expect(exchange.failure, isNotEmpty);

      // Retrying replaces it rather than leaving the failure behind.
      server.on('POST /v1/chats', chatJson(), status: 202);
      await conversation.retry(exchange);
      await settle();

      expect(conversation.exchanges.length, 1);
      expect(conversation.exchanges.single.rejected, isFalse);
      expect(conversation.exchanges.single.prompt, 'what is Go?');
    });

    test('ignores an empty prompt', () async {
      final (conversation, _, _) = await conversationOn(null, []);
      await conversation.send('   ');
      expect(conversation.exchanges, isEmpty);
    });
  });

  group('superseding', () {
    // Speaking again means the previous answer is no longer wanted. The
    // server cancels it too; doing it here as well is what makes the old
    // exchange dim at once rather than when its stream catches up.
    test(
      'dims the running exchange as soon as the next prompt is sent',
      () async {
        final (conversation, server, _) = await conversationOn(null, [
          // The first never finishes on its own.
          StreamAnswer(
            chunks: [sse('update', seq: 1, text: 'working')],
            hold: true,
          ),
          StreamAnswer(
            chunks: [sse('final', seq: 1, text: 'the second answer')],
          ),
        ]);
        server.on('POST /v1/chats', chatJson(), status: 202);
        server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'cancelled'));

        await conversation.send('first question');
        await settle(4);
        expect(conversation.exchanges.single.isRunning, isTrue);

        await conversation.send('no, this instead');

        expect(conversation.exchanges.length, 2);
        expect(conversation.exchanges.first.isRunning, isFalse);
        expect(conversation.exchanges.first.status, ChatStatus.cancelled);
      },
    );

    // A superseded prompt stays visible, struck through, so the
    // conversation still reads in the order it happened.
    test('a superseded exchange is marked rather than removed', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(
          chunks: [sse('update', seq: 1, text: 'working')],
          hold: true,
        ),
        StreamAnswer(chunks: [sse('final', seq: 1, text: 'second')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'cancelled'));

      await conversation.send('first');
      await settle(4);
      await conversation.send('second');
      await settle();

      expect(conversation.exchanges.first.isSuperseded, isTrue);
      expect(conversation.exchanges.first.prompt, 'first');
    });
  });

  group('stopping', () {
    test('cancels what is running and marks it at once', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(
          chunks: [sse('update', seq: 1, text: 'working')],
          hold: true,
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on(
        'POST /v1/chats/chat_01DDD/cancel',
        chatJson(status: 'cancelled'),
        status: 202,
      );
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'cancelled'));

      await conversation.send('a long job');
      await settle(4);

      await conversation.stop();

      expect(conversation.exchanges.single.status, ChatStatus.cancelled);
      expect(conversation.isIdle, isTrue);
    });

    // The chat finished while the cancel was in flight. That is not an
    // error the user should see.
    test('a chat that finished first is not reported as a failure', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(
          chunks: [sse('update', seq: 1, text: 'working')],
          hold: true,
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on('POST /v1/chats/chat_01DDD/cancel', {
        'error': 'That chat has already finished.',
      }, status: 409);
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'completed'));

      await conversation.send('a job');
      await settle(4);

      await conversation.stop();
      expect(conversation.exchanges.single.failure, isEmpty);
    });
  });

  group('a dropped stream', () {
    // A phone loses its connection constantly and the chat keeps running
    // regardless, so reconnecting is the normal case.
    test('reopens and asks to resume from what it already heard', () async {
      final (conversation, server, bytes) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('update', seq: 1, text: 'one')]),
        StreamAnswer(chunks: [sse('final', seq: 2, text: 'the answer')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'running'));

      await conversation.send('a question');
      // Long enough for the first stream to end and the reopen to happen.
      await Future<void>.delayed(const Duration(milliseconds: 100));
      await settle();

      expect(
        bytes.opened.length,
        greaterThanOrEqualTo(2),
        reason: 'the stream was not reopened',
      );
      expect(
        bytes.opened.last.headers['Last-Event-ID'],
        '1',
        reason: 'it did not ask to resume, so it would repeat itself',
      );
      expect(conversation.exchanges.single.answer, 'the answer');
    });

    // Reconnecting must not double up what was already shown.
    test('does not repeat what was already heard', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('update', seq: 1, text: 'one')]),
        StreamAnswer(
          chunks: [
            sse('update', seq: 2, text: 'two'),
            sse('final', seq: 3, text: 'done'),
          ],
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'running'));

      await conversation.send('a question');
      await Future<void>.delayed(const Duration(milliseconds: 100));
      await settle();

      expect(conversation.exchanges.single.updates, ['one', 'two']);
    });

    // The stream closing does not always mean the chat ended, so the
    // outcome has to be asked for rather than assumed.
    test('asks the server how it ended when the stream cannot say', () async {
      final (conversation, server, _) = await conversationOn(null, [
        StreamAnswer(chunks: [sse('update', seq: 1, text: 'one')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on(
        'GET /v1/chats/chat_01DDD',
        chatJson(status: 'completed', response: 'found it later'),
      );

      await conversation.send('a question');
      await Future<void>.delayed(const Duration(milliseconds: 100));
      await settle();

      expect(conversation.exchanges.single.answer, 'found it later');
      expect(conversation.exchanges.single.status, ChatStatus.completed);
    });
  });

  group('loading a session', () {
    test('restores the history oldest first', () async {
      final (conversation, server, _) = await conversationOn(null, []);
      server.on('GET /v1/sessions/sess_01CCC', {
        'session': sessionJson(),
        'chats': [
          chatJson(id: 'chat_01AAA', prompt: 'first', status: 'completed'),
          chatJson(id: 'chat_01BBB', prompt: 'second', status: 'completed'),
        ],
      });
      server.on('GET /v1/chats/chat_01AAA/messages', {
        'messages': [
          {'seq': 1, 'kind': 'update', 'text': 'looking', 'created_at': now},
        ],
      });
      server.on('GET /v1/chats/chat_01BBB/messages', {'messages': null});
      server.on(
        'GET /v1/chats/chat_01AAA',
        chatJson(id: 'chat_01AAA', status: 'completed', response: 'one'),
      );
      server.on(
        'GET /v1/chats/chat_01BBB',
        chatJson(id: 'chat_01BBB', status: 'completed', response: 'two'),
      );

      await conversation.load('sess_01CCC');
      await settle();

      expect(conversation.exchanges.map((e) => e.prompt), ['first', 'second']);
      expect(conversation.exchanges.first.updates, ['looking']);
      expect(conversation.exchanges.first.answer, 'one');
      expect(conversation.exchanges.last.answer, 'two');
      expect(conversation.loading, isFalse);
    });

    // Refreshing the page in the middle of an answer must not lose it.
    test('reattaches to a chat that was still running', () async {
      final (conversation, server, bytes) = await conversationOn(null, [
        StreamAnswer(
          chunks: [sse('final', seq: 2, text: 'finished after all')],
        ),
      ]);
      server.on('GET /v1/sessions/sess_01CCC', {
        'session': sessionJson(),
        'chats': [
          chatJson(id: 'chat_01DDD', prompt: 'still going', status: 'running'),
        ],
      });

      await conversation.load('sess_01CCC');
      await settle();

      expect(bytes.opened, isNotEmpty, reason: 'it did not reattach');
      expect(conversation.exchanges.single.answer, 'finished after all');
    });

    test('reports a session it cannot load, with a way to try again', () async {
      final (conversation, server, _) = await conversationOn(null, []);
      server.on('GET /v1/sessions/sess_01CCC', {
        'error': 'No such session.',
      }, status: 404);

      await conversation.load('sess_01CCC');

      expect(conversation.error, 'No such session.');
      expect(conversation.loading, isFalse);
    });
  });

  test('stops following once disposed', () async {
    final (conversation, server, bytes) = await conversationOn(null, [
      StreamAnswer(chunks: [sse('update', seq: 1, text: 'one')]),
    ]);
    server.on('POST /v1/chats', chatJson(), status: 202);

    await conversation.send('a question');
    await settle(2);
    conversation.dispose();
    await settle();

    // No exception from notifying a disposed listener, and nothing left open.
    expect(bytes.opened, isNotEmpty);
  });
}

/// Dropped : Stands in for a connection that could not be made.
class Dropped implements Exception {
  const Dropped();
}
