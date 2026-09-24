import 'dart:async';
import 'dart:convert';

import 'package:friday_client/friday/friday.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A fake FRIDAY, so the client can be tested without a server.
///
/// It answers from canned bodies rather than behaving like FRIDAY: what is
/// under test is the client's handling of what a server sends, and a fake
/// that reimplemented the server would only test itself.

/// now : A fixed timestamp, so a body is the same on every run.
const String now = '2026-09-21T10:00:00Z';

/// Recorded : One request the client made.
class Recorded {
  Recorded(this.method, this.url, this.headers, this.body);

  final String method;
  final Uri url;
  final Map<String, String> headers;
  final String body;

  /// json : The request body, decoded.
  Map<String, dynamic> get json => jsonDecode(body) as Map<String, dynamic>;
}

/// FakeServer : Answers the client's requests and records them.
class FakeServer {
  FakeServer();

  /// requests : Every request made, in order.
  final List<Recorded> requests = [];

  /// _routes : What to answer, keyed by "METHOD /path".
  final Map<String, http.Response Function(Recorded)> _routes = {};

  /// on : Answers a route with a JSON body and status.
  void on(String route, Object body, {int status = 200}) {
    _routes[route] = (_) => http.Response(
      body is String ? body : jsonEncode(body),
      status,
      headers: {'content-type': 'application/json'},
    );
  }

  /// onRequest : Answers a route with whatever the callback decides.
  void onRequest(String route, http.Response Function(Recorded) answer) {
    _routes[route] = answer;
  }

  /// client : An http.Client wired to this fake.
  http.Client get client => MockClient((request) async {
    final recorded = Recorded(
      request.method,
      request.url,
      request.headers,
      request.body,
    );
    requests.add(recorded);

    final answer = _routes['${request.method} ${request.url.path}'];
    if (answer == null) {
      return http.Response(
        jsonEncode({'error': 'No such route in the fake.'}),
        404,
        headers: {'content-type': 'application/json'},
      );
    }
    return answer(recorded);
  });

  /// last : The most recent request.
  Recorded get last => requests.last;
}

/// FakeByteSource : Hands back a canned stream instead of opening a socket.
class FakeByteSource implements ByteSource {
  FakeByteSource({this.status = 200, this.chunks = const [], this.error});

  /// status : What the response reports.
  int status;

  /// chunks : The body, one string per chunk.
  List<String> chunks;

  /// error : When set, opening throws it instead of answering.
  Object? error;

  /// opened : The URL and headers of each open, so a test can check that the
  /// token and the resume position were sent.
  final List<({Uri url, Map<String, String> headers})> opened = [];

  /// closed : Whether close has been called.
  bool closed = false;

  @override
  Future<StreamedResponse> open(Uri url, Map<String, String> headers) async {
    opened.add((url: url, headers: headers));
    if (error != null) throw error!;
    return StreamedResponse(
      statusCode: status,
      body: Stream.fromIterable(chunks.map(utf8.encode)),
      contentType: 'text/event-stream',
    );
  }

  @override
  void close() => closed = true;
}

/// ScriptedByteSource : Answers each stream open from a queue, so a test
/// can give one answer to the first connection and a different one to the
/// reconnection after it drops.
class ScriptedByteSource implements ByteSource {
  ScriptedByteSource(this.answers);

  /// answers : One entry per expected open. The last is reused once
  /// exhausted, so a test need not script reconnections it does not care
  /// about.
  final List<StreamAnswer> answers;

  /// opened : Each open's URL and headers, for checking that a resume asked
  /// to resume.
  final List<({Uri url, Map<String, String> headers})> opened = [];

  bool closed = false;
  int _next = 0;

  @override
  Future<StreamedResponse> open(Uri url, Map<String, String> headers) async {
    opened.add((url: url, headers: headers));
    final answer = answers[_next < answers.length ? _next : answers.length - 1];
    _next++;

    if (answer.error != null) throw answer.error!;
    return StreamedResponse(
      statusCode: answer.status,
      body: answer.stream(),
      contentType: 'text/event-stream',
    );
  }

  @override
  void close() => closed = true;
}

/// StreamAnswer : What one stream open produces.
class StreamAnswer {
  StreamAnswer({
    this.status = 200,
    this.chunks = const [],
    this.error,
    this.hold = false,
    this.pushed,
  });

  final int status;
  final List<String> chunks;

  /// error : Thrown instead of answering, standing in for a connection that
  /// could not be made.
  final Object? error;

  /// hold : Whether the stream stays open after its chunks, as a real one
  /// does while the chat is still running.
  ///
  /// Without this a scripted stream ends immediately, the client concludes
  /// the connection dropped and asks how the chat finished — so a test
  /// meaning to observe a chat mid-run observes a finished one instead,
  /// and passes for the wrong reason.
  final bool hold;

  /// manual : An answer whose chunks the test pushes in itself, for when
  /// what matters is an event arriving at a particular moment.
  static (StreamAnswer, StreamController<List<int>>) manual() {
    final controller = StreamController<List<int>>();
    return (StreamAnswer(pushed: controller), controller);
  }

  /// pushed : When set, the body is whatever the test puts into it.
  final StreamController<List<int>>? pushed;

  /// stream : The body.
  Stream<List<int>> stream() {
    final pushed = this.pushed;
    if (pushed != null) return pushed.stream;

    final controller = StreamController<List<int>>();
    unawaited(() async {
      for (final chunk in chunks) {
        // A gap between chunks, so a test can observe the intermediate
        // state rather than only the end.
        await Future<void>.delayed(Duration.zero);
        if (controller.isClosed) return;
        controller.add(utf8.encode(chunk));
      }
      if (!hold && !controller.isClosed) await controller.close();
    }());
    return controller.stream;
  }
}

/// user, client, session : Canned objects matching the server's wire shapes.
Map<String, dynamic> user({String id = 'usr_01AAA', String name = 'dhanush'}) =>
    {'id': id, 'username': name, 'created_at': now};

Map<String, dynamic> clientJson({
  String id = 'cli_01BBB',
  String name = 'my phone',
  bool current = true,
  bool revoked = false,
  String activeSession = 'sess_01CCC',
}) => {
  'id': id,
  'name': name,
  'current': current,
  'revoked': revoked,
  'active_session_id': activeSession,
  'created_at': now,
};

Map<String, dynamic> sessionJson({
  String id = 'sess_01CCC',
  String title = '',
  bool active = true,
}) => {
  'id': id,
  'title': title,
  'active': active,
  'created_at': now,
  'updated_at': now,
};

Map<String, dynamic> chatJson({
  String id = 'chat_01DDD',
  String session = 'sess_01CCC',
  String prompt = 'a question',
  String status = 'pending',
  String response = '',
  String error = '',
}) => {
  'id': id,
  'session_id': session,
  'prompt': prompt,
  'status': status,
  if (response.isNotEmpty) 'response': response,
  if (error.isNotEmpty) 'error': error,
  'created_at': now,
  'updated_at': now,
};

/// sse : One server-sent event frame, as FRIDAY writes it.
String sse(String kind, {int? seq, String text = ''}) {
  final payload = jsonEncode({
    'kind': kind,
    'seq': ?seq,
    if (text.isNotEmpty) 'text': text,
    'at': now,
  });
  return '${seq != null ? 'id: $seq\n' : ''}event: $kind\ndata: $payload\n\n';
}

/// loggedIn : An API already holding a token, for the calls that need one.
FridayApi loggedIn(FakeServer server, {ByteSource? bytes}) {
  final api = FridayApi(
    baseUrl: Uri.parse('https://friday.test'),
    httpClient: server.client,
    byteSource: bytes ?? FakeByteSource(),
    tokens: InMemoryTokenStore(),
  );
  server.on('POST /v1/auth/login', {
    'token': 'a-token',
    'user': user(),
    'client': clientJson(),
  }, status: 201);
  return api;
}

/// authorise : Logs the API in, so later calls carry a token.
Future<void> authorise(FridayApi api) async {
  await api.login(username: 'dhanush', password: 'a password');
}
