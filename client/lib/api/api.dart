/// Talking to the assistant.
library;

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'byte_source.dart';
import 'errors.dart';
import 'models.dart';
import 'token_store.dart';

/// _defaultTimeout : How long an ordinary request may take.
///
/// Generous, because the client is a phone on mobile data rather than a
/// service on a fast network.
const Duration _defaultTimeout = Duration(seconds: 20);

/// _waitMargin : Added to a requested wait to get the request's timeout.
///
/// Asking the server to hold the connection for thirty seconds and then
/// giving up at twenty would abandon an answer that was about to arrive.
const Duration _waitMargin = Duration(seconds: 10);

/// AssistantApi : A client for the assistant's HTTP interface.
///
/// One instance per server. It holds the bearer token, so everything above
/// it is free of authentication, and it turns every failure into one of the
/// types in errors.dart, so a caller never has to read a status code.
class AssistantApi {
  AssistantApi({
    required this.baseUrl,
    http.Client? httpClient,
    ByteSource? byteSource,
    TokenStore? tokens,
    this.timeout = _defaultTimeout,
  }) : _http = httpClient ?? http.Client(),
       _bytes = byteSource ?? defaultByteSource(),
       _tokens = tokens ?? InMemoryTokenStore();

  /// baseUrl : Where the assistant is, such as `https://friday-server.duckdns.org`.
  final Uri baseUrl;

  /// timeout : How long an ordinary request may take. A request that asks
  /// the server to hold the connection gets that long plus a margin instead.
  final Duration timeout;

  final http.Client _http;
  final ByteSource _bytes;
  final TokenStore _tokens;

  String? _token;

  /// hasToken : Whether there is a token to present. False does not mean the
  /// token is good, only that there is one.
  bool get hasToken => _token != null;

  /// restore : Loads a token kept from a previous run, returning whether
  /// there was one.
  Future<bool> restore() async {
    _token = await _tokens.read();
    return _token != null;
  }

  /// login : Authenticates and registers this client, keeping the token it
  /// is given.
  ///
  /// Logging in and registering are one act on the server: a token exists
  /// only for a client. [clientName] is what the client is called in a
  /// listing, such as "my phone".
  Future<LoginResult> login({
    required String username,
    required String password,
    String? clientName,
    String? clientId,
  }) async {
    final body = await _send(
      'POST',
      '/v1/auth/login',
      body: {
        'username': username,
        'password': password,
        if (clientName != null && clientName.isNotEmpty)
          'client_name': clientName,
        if (clientId != null && clientId.isNotEmpty) 'client_id': clientId,
      },
      authenticated: false,
    );

    final result = LoginResult.fromJson(body);
    _token = result.token;
    await _tokens.write(result.token);
    return result;
  }

  /// logout : Forgets the token locally.
  ///
  /// It does not revoke it on the server — that is [revokeClient], and is a
  /// different decision: logging out of a browser should not stop the phone
  /// working.
  Future<void> logout() async {
    _token = null;
    await _tokens.clear();
  }

  /// me : Returns who is calling, from what, and where a prompt will land.
  Future<Identity> me() async =>
      Identity.fromJson(await _send('GET', '/v1/me'));

  /// listClients : Returns the user's clients, revoked ones included.
  Future<List<Client>> listClients({bool revoked = false}) async => parseList(
    await _send(
      'GET',
      '/v1/clients',
      query: {if (revoked) 'revoked': 'true'},
    ),
    'clients',
    Client.fromJson,
  );

  /// setClientChannel : Changes how a client's prompts are treated.
  ///
  /// A client says what it is when it registers, and some cannot: Home
  /// Assistant is handed a token through a screen with no field for it. This
  /// is how that is corrected.
  Future<List<Client>> setClientChannel(String clientId, String channel) async =>
      parseList(
        await _send(
          'POST',
          '/v1/clients/$clientId/channel',
          body: {'channel': channel},
        ),
        'clients',
        Client.fromJson,
      );

  /// listModels : Returns the models a client can be set to answer with.
  ///
  /// Only what the server's provider can reach, so a choice offered here is
  /// one that will work rather than one that fails the next time the person
  /// speaks.
  Future<List<LlmModel>> listModels() async =>
      parseList(await _send('GET', '/v1/models'), 'models', LlmModel.fromJson);

  /// setClientModel : Chooses which model answers a client's prompts.
  ///
  /// An empty [model] clears the choice, putting the client back on whatever
  /// the server is configured with.
  Future<List<Client>> setClientModel(
    String clientId,
    String vendor,
    String model,
  ) async => parseList(
    await _send(
      'POST',
      '/v1/clients/$clientId/model',
      body: {'vendor': vendor, 'model': model},
    ),
    'clients',
    Client.fromJson,
  );

  /// revokeClient : Stops one of the user's clients authenticating. Any of
  /// them may revoke any other, which is how a lost phone is dealt with.
  Future<void> revokeClient(String clientId) =>
      _send('DELETE', '/v1/clients/$clientId', expectBody: false);

  /// createConversation : Starts a new thread. It becomes this client's active
  /// one unless [activate] says otherwise.
  Future<Conversation> createConversation({String? title, bool activate = true}) async =>
      Conversation.fromJson(
        await _send(
          'POST',
          '/v1/conversations',
          body: {
            if (title != null && title.isNotEmpty) 'title': title,
            'activate': activate,
          },
        ),
      );

  /// listConversations : Returns conversations, most recently used first.
  Future<List<Conversation>> listConversations({int? limit, bool archived = false}) async => parseList(
    await _send(
      'GET',
      '/v1/conversations',
      query: {
        if (limit != null) 'limit': '$limit',
        if (archived) 'archived': 'true',
      },
    ),
    'conversations',
    Conversation.fromJson,
  );

  /// conversation : Returns a conversation with its chats, oldest first.
  Future<ConversationDetail> conversation(String conversationId) async =>
      ConversationDetail.fromJson(await _send('GET', '/v1/conversations/$conversationId'));

  /// activateConversation : Moves this client into a conversation. Other clients of
  /// the same user stay where they are.
  Future<Conversation> activateConversation(String conversationId) async =>
      Conversation.fromJson(await _send('POST', '/v1/conversations/$conversationId/activate'));

  /// archiveConversation : Puts a conversation away, or brings it back.
  ///
  /// Archiving the conversation this client is in leaves it nowhere to talk, so the
  /// server starts a fresh one and returns it. Unarchiving returns the conversation
  /// itself, since nothing moved.
  Future<Conversation> archiveConversation(String conversationId, {bool archived = true}) async {
    final path = archived ? 'archive' : 'unarchive';
    final json = await _send('POST', '/v1/conversations/$conversationId/$path');
    return Conversation.fromJson(
      archived ? json['active'] as Map<String, dynamic> : json,
    );
  }

  /// deleteConversation : Removes a conversation and everything said in it.
  ///
  /// Nothing here can be undone. Returns the conversation this client is in
  /// afterwards, which is a fresh one when the deleted conversation was the one it
  /// was using.
  Future<Conversation> deleteConversation(String conversationId) async {
    final json = await _send('DELETE', '/v1/conversations/$conversationId');
    return Conversation.fromJson(json['active'] as Map<String, dynamic>);
  }

  /// renameConversation : Changes what a conversation is called.
  ///
  /// An empty title clears the name rather than being refused, so a name
  /// given by mistake can be taken off without deleting the conversation.
  Future<Conversation> renameConversation(String conversationId, String title) async =>
      Conversation.fromJson(await _send(
        'POST',
        '/v1/conversations/$conversationId/rename',
        body: {'title': title},
      ));

  /// ask : Sends a prompt and reads the answer as it arrives.
  ///
  /// One endpoint, Ollama's shape, and it answers only when the chat is over.
  /// The chunks come back as newline-delimited JSON: each carries a piece of
  /// what to say, and the last carries done together with the failure's code
  /// and detail when there was one.
  ///
  /// Sending a prompt supersedes whatever is still running in the same
  /// conversation, so a correction cancels the question it corrects.
  Stream<AnswerChunk> ask(String prompt, {String? conversationId}) async* {
    final token = _token;
    if (token == null) throw const NotAuthenticated();

    final StreamedResponse response;
    try {
      response = await _bytes.post(
        _url('/api/chat'),
        {
          'Accept': 'application/x-ndjson',
          'Content-Type': 'application/json',
          'Authorization': 'Bearer $token',
        },
        jsonEncode({
          'model': 'assistant',
          'messages': [
            {'role': 'user', 'content': prompt},
          ],
          if (conversationId != null && conversationId.isNotEmpty) 'conversation_id': conversationId,
        }),
      );
    } on Object catch (e) {
      throw Unreachable('I cannot reach the assistant at the moment.', e);
    }

    if (response.statusCode != 200) {
      // A refusal's body is short, so reading it whole is safe here in a way
      // it would not be for the answer itself.
      final body = await utf8.decodeStream(response.body);
      throw _failureFor(response.statusCode, body);
    }

    var buffer = '';
    await for (final bytes in response.body) {
      buffer += utf8.decode(bytes, allowMalformed: true);
      while (true) {
        final end = buffer.indexOf('\n');
        if (end < 0) break;
        final line = buffer.substring(0, end).trim();
        buffer = buffer.substring(end + 1);
        if (line.isEmpty) continue;
        yield AnswerChunk.fromJson(jsonDecode(line) as Map<String, dynamic>);
      }
    }
  }

  /// conversation, so a correction cancels the question it corrects.
  Future<Chat> createChat(
    String prompt, {
    String? conversationId,
    Duration? wait,
  }) async => Chat.fromJson(
    await _send(
      'POST',
      '/v1/chats',
      query: {if (wait != null) 'wait': _duration(wait)},
      body: {
        'prompt': prompt,
        if (conversationId != null && conversationId.isNotEmpty) 'conversation_id': conversationId,
      },
      overrideTimeout: wait == null ? null : wait + _waitMargin,
    ),
  );

  /// chat : Returns one chat, including its answer once it has one.
  Future<Chat> chat(String chatId) async =>
      Chat.fromJson(await _send('GET', '/v1/chats/$chatId'));

  /// listChats : Returns recent chats, newest first, without their answers.
  Future<List<ChatSummary>> listChats({ChatStatus? status, int? limit}) async =>
      parseList(
        await _send(
          'GET',
          '/v1/chats',
          query: {
            if (status != null && status != ChatStatus.unknown)
              'status': status.wire,
            if (limit != null) 'limit': '$limit',
          },
        ),
        'chats',
        ChatSummary.fromJson,
      );

  /// cancelChat : Stops a chat that has not finished. This is what "stop"
  /// does while the assistant is speaking.
  Future<Chat> cancelChat(String chatId) async =>
      Chat.fromJson(await _send('POST', '/v1/chats/$chatId/cancel'));

  /// close : Releases the connections this client holds.
  void close() {
    _http.close();
    _bytes.close();
  }

  /// _send : Issues a request and returns its decoded body.
  Future<Map<String, dynamic>> _send(
    String method,
    String path, {
    Map<String, String> query = const {},
    Object? body,
    bool authenticated = true,
    bool expectBody = true,
    Duration? overrideTimeout,
  }) async {
    final token = _token;
    if (authenticated && token == null) throw const NotAuthenticated();

    final request = http.Request(method, _url(path, query))
      ..headers['Accept'] = 'application/json';
    if (authenticated) request.headers['Authorization'] = 'Bearer $token';
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }

    final http.Response response;
    try {
      final streamed = await _http
          .send(request)
          .timeout(overrideTimeout ?? timeout);
      response = await http.Response.fromStream(streamed);
    } on TimeoutException catch (e) {
      throw Unreachable('The assistant took too long to answer.', e);
    } on Object catch (e) {
      throw Unreachable('I cannot reach the assistant at the moment.', e);
    }

    if (response.statusCode >= 400) {
      final failure = _failureFor(response.statusCode, response.body);
      // A refused token will not become valid, so it is dropped rather than
      // presented again on every later call.
      if (failure is NotAuthenticated) await logout();
      throw failure;
    }

    if (!expectBody || response.body.isEmpty) return const {};

    try {
      final decoded = jsonDecode(response.body);
      if (decoded is! Map<String, dynamic>) {
        throw BadResponse(
          'The assistant sent something unexpected.',
          body: _snippet(response.body),
        );
      }
      return decoded;
    } on FormatException {
      throw BadResponse(
        'The assistant sent something I could not read.',
        body: _snippet(response.body),
      );
    }
  }

  /// _failureFor : Turns a failing response into the exception a caller can
  /// act on, preferring the server's own wording since it is written to be
  /// read aloud.
  ApiException _failureFor(int status, String body) {
    final message = _messageIn(body);
    return switch (status) {
      401 || 403 => NotAuthenticated(message ?? 'You need to log in again.'),
      404 => NotFound(message ?? 'That does not exist.'),
      409 => Conflict(message ?? 'That has already finished.'),
      >= 400 && < 500 => Refused(
        message ?? 'The assistant would not accept that.',
        statusCode: status,
      ),
      _ => ServerFailure(
        message ?? 'Something went wrong on the assistant\'s end.',
        statusCode: status,
      ),
    };
  }

  /// _messageIn : Reads the message out of an error body, if there is one.
  String? _messageIn(String body) {
    if (body.isEmpty) return null;
    try {
      final decoded = jsonDecode(body);
      if (decoded is Map && decoded['error'] is String) {
        final message = decoded['error'] as String;
        return message.isEmpty ? null : message;
      }
    } on FormatException {
      // Not JSON, which happens when something in front of the assistant answers
      // instead of the assistant. There is nothing quotable in it.
    }
    return null;
  }

  /// _url : Builds an absolute URL, keeping the base path if there is one.
  Uri _url(String path, [Map<String, String> query = const {}]) {
    final base = baseUrl.path.endsWith('/')
        ? baseUrl.path.substring(0, baseUrl.path.length - 1)
        : baseUrl.path;
    return baseUrl.replace(
      path: '$base$path',
      queryParameters: query.isEmpty ? null : query,
    );
  }

  /// _duration : Renders a duration the way Go parses one.
  String _duration(Duration d) => '${d.inMilliseconds}ms';

  /// _snippet : Shortens a body for an error, so a diagnostic cannot carry a
  /// whole response.
  String _snippet(String body) =>
      body.length <= 200 ? body : '${body.substring(0, 200)}…';
}
