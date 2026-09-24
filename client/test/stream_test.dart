import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/friday/friday.dart';

import 'support.dart';

void main() {
  test('yields the updates then the answer, and ends', () async {
    final bytes = FakeByteSource(
      chunks: [
        sse('update', seq: 1, text: 'Let me take a look.'),
        sse('update', seq: 2, text: 'Still working on it.'),
        sse('final', seq: 3, text: 'Two of them look risky.'),
      ],
    );
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    final got = await api.streamChat('chat_01DDD').toList();

    expect(got.map((e) => e.text), [
      'Let me take a look.',
      'Still working on it.',
      'Two of them look risky.',
    ]);
    expect(got.last.isTerminal, isTrue);
  });

  // The stream is the one endpoint the browser's EventSource cannot reach,
  // because it cannot set this header. It must be sent.
  test('presents the token as a header, never in the URL', () async {
    final bytes = FakeByteSource(chunks: [sse('final', seq: 1, text: 'done')]);
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    await api.streamChat('chat_01DDD').toList();

    final opened = bytes.opened.single;
    expect(opened.headers['Authorization'], 'Bearer a-token');
    expect(opened.headers['Accept'], 'text/event-stream');
    expect(opened.url.toString(), isNot(contains('a-token')));
    expect(opened.url.path, '/v1/chats/chat_01DDD/stream');
  });

  // Hearing the same thing twice would mean a voice client repeating itself.
  test('asks to resume from where it left off', () async {
    final bytes = FakeByteSource(chunks: [sse('final', seq: 4, text: 'rest')]);
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    await api.streamChat('chat_01DDD', resumeFrom: 3).toList();

    expect(bytes.opened.single.headers['Last-Event-ID'], '3');
  });

  test('does not ask to resume from the beginning', () async {
    final bytes = FakeByteSource(chunks: [sse('final', seq: 1, text: 'done')]);
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    await api.streamChat('chat_01DDD').toList();

    expect(bytes.opened.single.headers.containsKey('Last-Event-ID'), isFalse);
  });

  test('refuses to open a stream without a token', () async {
    final server = FakeServer();
    final api = FridayApi(
      baseUrl: Uri.parse('https://friday.test'),
      httpClient: server.client,
      byteSource: FakeByteSource(),
    );

    await expectLater(
      api.streamChat('chat_01DDD'),
      emitsError(isA<NotAuthenticated>()),
    );
  });

  // A refusal arrives as a status and a short JSON body rather than as
  // events, so it has to be read and turned into the same exception the
  // ordinary calls produce.
  test('turns a refused stream into the matching failure', () async {
    final cases = {
      404: isA<NotFound>(),
      401: isA<NotAuthenticated>(),
      500: isA<ServerFailure>(),
    };

    for (final entry in cases.entries) {
      final bytes = FakeByteSource(
        status: entry.key,
        chunks: ['{"error":"No such chat."}'],
      );
      final server = FakeServer();
      final api = loggedIn(server, bytes: bytes);
      await authorise(api);

      await expectLater(
        api.streamChat('chat_01ZZZ'),
        emitsError(entry.value),
        reason: 'status ${entry.key}',
      );
    }
  });

  test('reports a connection that could not be opened', () async {
    final bytes = FakeByteSource(error: const SocketishError());
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    await expectLater(
      api.streamChat('chat_01DDD'),
      emitsError(isA<Unreachable>()),
    );
  });

  // A chat that finished before anyone listened replays what it said and
  // how it ended, rather than producing an empty stream.
  test('replays a chat that had already finished', () async {
    final bytes = FakeByteSource(
      chunks: [
        sse('update', seq: 1, text: 'one'),
        sse('update', seq: 2, text: 'two'),
        sse('final', seq: 3, text: 'the answer'),
      ],
    );
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    final got = await api.streamChat('chat_01DDD').toList();
    expect(got.length, 3);
    expect(got.last.kind, EventKind.finalAnswer);
  });

  test('ends on a failure with the reason, which is what is spoken', () async {
    final bytes = FakeByteSource(
      chunks: [
        sse('update', seq: 1, text: 'trying'),
        sse('error', text: 'I could not reach GitLab.'),
      ],
    );
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    final got = await api.streamChat('chat_01DDD').toList();

    expect(got.last.kind, EventKind.error);
    expect(got.last.text, 'I could not reach GitLab.');
    expect(got.last.isTerminal, isTrue);
  });

  test('ends on cancellation, which is what "stop" produces', () async {
    final bytes = FakeByteSource(
      chunks: [
        sse('update', seq: 1, text: 'working'),
        sse('cancelled'),
      ],
    );
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);
    await authorise(api);

    final got = await api.streamChat('chat_01DDD').toList();
    expect(got.last.kind, EventKind.cancelled);
    expect(got.last.isTerminal, isTrue);
  });

  test('closing releases the stream source', () async {
    final bytes = FakeByteSource();
    final server = FakeServer();
    final api = loggedIn(server, bytes: bytes);

    api.close();
    expect(bytes.closed, isTrue);
  });
}

/// SocketishError : Stands in for whatever the transport throws.
class SocketishError implements Exception {
  const SocketishError();
}
