import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/friday/friday.dart';
import 'package:http/http.dart' as http;

import 'support.dart';

void main() {
  group('login', () {
    test('keeps the token and presents it afterwards', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      server.on('GET /v1/me', {'user': user(), 'client': clientJson()});

      final result = await api.login(
        username: 'dhanush',
        password: 'a password',
        clientName: 'my phone',
      );

      expect(result.token, 'a-token');
      expect(result.user.username, 'dhanush');
      expect(api.hasToken, isTrue);

      // The login itself must not carry an Authorization header: it is what
      // issues the token.
      expect(server.requests.first.headers, isNot(contains('Authorization')));
      expect(server.requests.first.json['client_name'], 'my phone');

      await api.me();
      expect(server.last.headers['Authorization'], 'Bearer a-token');
    });

    test('omits an empty client name rather than sending one', () async {
      final server = FakeServer();
      final api = loggedIn(server);

      await api.login(username: 'dhanush', password: 'p', clientName: '');
      expect(server.last.json.containsKey('client_name'), isFalse);
    });

    test('reports wrong credentials as needing to log in', () async {
      final server = FakeServer();
      final api = FridayApi(
        baseUrl: Uri.parse('https://friday.test'),
        httpClient: server.client,
        byteSource: FakeByteSource(),
      );
      server.on('POST /v1/auth/login', {
        'error': 'That username or password is not correct.',
      }, status: 401);

      await expectLater(
        api.login(username: 'dhanush', password: 'wrong'),
        throwsA(
          isA<NotAuthenticated>().having(
            (e) => e.message,
            'message',
            'That username or password is not correct.',
          ),
        ),
      );
      expect(api.hasToken, isFalse);
    });

    // Printing a login result must not put a credential in a log.
    test('does not print the token', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      final result = await api.login(username: 'd', password: 'p');

      expect(result.toString(), isNot(contains('a-token')));
    });
  });

  group('token handling', () {
    test('refuses to call an authenticated endpoint without one', () async {
      final server = FakeServer();
      final api = FridayApi(
        baseUrl: Uri.parse('https://friday.test'),
        httpClient: server.client,
        byteSource: FakeByteSource(),
      );

      await expectLater(api.me(), throwsA(isA<NotAuthenticated>()));
      // Nothing was sent: there was nothing to send it with.
      expect(server.requests, isEmpty);
    });

    // A refused token never becomes valid again, so presenting it on every
    // later call would just produce a string of 401s.
    test('drops a token the server refuses', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/me', {
        'error': 'That token is not valid.',
      }, status: 401);

      await expectLater(api.me(), throwsA(isA<NotAuthenticated>()));
      expect(api.hasToken, isFalse);
    });

    test('restores a token kept from a previous run', () async {
      final server = FakeServer();
      final store = InMemoryTokenStore();
      await store.write('kept-token');

      final api = FridayApi(
        baseUrl: Uri.parse('https://friday.test'),
        httpClient: server.client,
        byteSource: FakeByteSource(),
        tokens: store,
      );
      server.on('GET /v1/me', {'user': user(), 'client': clientJson()});

      expect(await api.restore(), isTrue);
      await api.me();
      expect(server.last.headers['Authorization'], 'Bearer kept-token');
    });

    test('logging out forgets the token without revoking it', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);

      await api.logout();

      expect(api.hasToken, isFalse);
      // Only the login was sent; logging out is local.
      expect(server.requests.length, 1);
    });
  });

  group('chats', () {
    test('sends a prompt and reads back the chat', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('POST /v1/chats', chatJson(), status: 202);

      final chat = await api.createChat('check my merge requests');

      expect(chat.id, 'chat_01DDD');
      expect(chat.status, ChatStatus.pending);
      expect(chat.status.isTerminal, isFalse);
      expect(server.last.json['prompt'], 'check my merge requests');
      expect(server.last.json.containsKey('session_id'), isFalse);
    });

    test('continues a named session', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await api.createChat('and again', sessionId: 'sess_01CCC');
      expect(server.last.json['session_id'], 'sess_01CCC');
    });

    // The server parses Go durations, so a Dart Duration has to be written
    // in a form it accepts.
    test('writes a wait the way the server parses it', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on(
        'POST /v1/chats',
        chatJson(status: 'completed', response: 'the answer'),
      );

      final chat = await api.createChat(
        'answer me now',
        wait: const Duration(seconds: 5),
      );

      expect(server.last.url.queryParameters['wait'], '5000ms');
      expect(chat.status, ChatStatus.completed);
      expect(chat.response, 'the answer');
    });

    test(
      'reports an empty prompt as refused, with the server wording',
      () async {
        final server = FakeServer();
        final api = loggedIn(server);
        await authorise(api);
        server.on('POST /v1/chats', {
          'error': 'A prompt is required.',
        }, status: 400);

        await expectLater(
          api.createChat(''),
          throwsA(
            isA<Refused>()
                .having((e) => e.message, 'message', 'A prompt is required.')
                .having((e) => e.statusCode, 'statusCode', 400),
          ),
        );
      },
    );

    test('reports an unknown chat as not found', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/chats/chat_01ZZZ', {
        'error': 'No such chat.',
      }, status: 404);

      await expectLater(api.chat('chat_01ZZZ'), throwsA(isA<NotFound>()));
    });

    test('reports cancelling a finished chat as a conflict', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('POST /v1/chats/chat_01DDD/cancel', {
        'error': 'That chat has already finished.',
      }, status: 409);

      await expectLater(api.cancelChat('chat_01DDD'), throwsA(isA<Conflict>()));
    });

    test('lists chats and their filters', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/chats', {
        'chats': [chatJson(status: 'completed'), chatJson(id: 'chat_01EEE')],
      });

      final chats = await api.listChats(status: ChatStatus.completed, limit: 5);

      expect(chats.length, 2);
      expect(chats.first.status, ChatStatus.completed);
      expect(server.last.url.queryParameters, {
        'status': 'completed',
        'limit': '5',
      });
    });

    test('reads the messages a chat produced', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/chats/chat_01DDD/messages', {
        'messages': [
          {'seq': 1, 'kind': 'update', 'text': 'one', 'created_at': now},
          {'seq': 2, 'kind': 'final', 'text': 'two', 'created_at': now},
        ],
      });

      final messages = await api.messages('chat_01DDD');

      expect(messages.map((m) => m.text), ['one', 'two']);
      expect(messages.last.kind, EventKind.finalAnswer);
    });

    // A listing with nothing in it sends null rather than an empty array,
    // which must read as empty rather than throwing.
    test('reads an empty listing', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/chats', {'chats': null});

      expect(await api.listChats(), isEmpty);
    });
  });

  group('sessions and clients', () {
    test('creates a session and reads it back with its chats', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on(
        'POST /v1/sessions',
        sessionJson(title: 'groceries'),
        status: 201,
      );
      server.on('GET /v1/sessions/sess_01CCC', {
        'session': sessionJson(title: 'groceries'),
        'chats': [chatJson(), chatJson(id: 'chat_01EEE')],
      });

      final created = await api.createSession(title: 'groceries');
      expect(created.title, 'groceries');
      expect(created.active, isTrue);
      expect(server.last.json['activate'], true);

      final detail = await api.session('sess_01CCC');
      expect(detail.chats.length, 2);
    });

    test('can create a session without activating it', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('POST /v1/sessions', sessionJson(active: false), status: 201);

      await api.createSession(title: 'later', activate: false);
      expect(server.last.json['activate'], false);
    });

    test('revoking a client expects no body back', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.onRequest(
        'DELETE /v1/clients/cli_01BBB',
        (_) => http.Response('', 204),
      );

      await api.revokeClient('cli_01BBB');
      expect(server.last.method, 'DELETE');
    });

    test('lists clients, revoked ones included', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/clients', {
        'clients': [
          clientJson(),
          clientJson(id: 'cli_01OLD', current: false, revoked: true),
        ],
      });

      final clients = await api.listClients();
      expect(clients.length, 2);
      expect(clients.first.current, isTrue);
      expect(clients.last.revoked, isTrue);
    });
  });

  group('failures', () {
    test('turns a transport failure into Unreachable', () async {
      final server = FakeServer();
      server.onRequest('GET /v1/me', (_) => throw const SocketishError());
      final api = loggedIn(server);
      await authorise(api);

      await expectLater(api.me(), throwsA(isA<Unreachable>()));
    });

    test('reports a server failure as retryable when it is', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('POST /v1/chats', {
        'error': 'FRIDAY is not accepting work at the moment.',
      }, status: 503);

      await expectLater(
        api.createChat('hello'),
        throwsA(
          isA<ServerFailure>().having(
            (e) => e.isRetryable,
            'isRetryable',
            isTrue,
          ),
        ),
      );
    });

    // A 500 must not carry the cause to the user; the server puts it in its
    // own log instead.
    test('does not invent detail for a bare 500', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.on('GET /v1/me', {
        'error': 'Something went wrong on my end.',
      }, status: 500);

      await expectLater(
        api.me(),
        throwsA(
          isA<ServerFailure>()
              .having((e) => e.statusCode, 'statusCode', 500)
              .having((e) => e.isRetryable, 'isRetryable', isFalse),
        ),
      );
    });

    // Something in front of FRIDAY — a proxy, a captive portal — can answer
    // instead, and it will not answer in FRIDAY's shape.
    test('handles a non-JSON body from something that is not FRIDAY', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.onRequest(
        'GET /v1/me',
        (_) => http.Response(
          '<html>502 Bad Gateway</html>',
          502,
          headers: {'content-type': 'text/html'},
        ),
      );

      await expectLater(
        api.me(),
        throwsA(
          isA<ServerFailure>().having(
            (e) => e.message,
            'message',
            isNot(contains('<html>')),
          ),
        ),
      );
    });

    test('reports an unreadable success body', () async {
      final server = FakeServer();
      final api = loggedIn(server);
      await authorise(api);
      server.onRequest(
        'GET /v1/me',
        (_) => http.Response(
          'not json',
          200,
          headers: {'content-type': 'application/json'},
        ),
      );

      await expectLater(api.me(), throwsA(isA<BadResponse>()));
    });
  });

  group('url building', () {
    test('keeps a base path, so FRIDAY can live under a prefix', () async {
      final server = FakeServer();
      final api = FridayApi(
        baseUrl: Uri.parse('https://friday.test/api/'),
        httpClient: server.client,
        byteSource: FakeByteSource(),
      );
      server.on('POST /api/v1/auth/login', {
        'token': 't',
        'user': user(),
        'client': clientJson(),
      }, status: 201);

      await api.login(username: 'd', password: 'p');
      expect(server.last.url.path, '/api/v1/auth/login');
    });
  });
}

/// SocketishError : Stands in for whatever the transport throws.
class SocketishError implements Exception {
  const SocketishError();
}
