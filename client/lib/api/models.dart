/// The wire shapes the assistant publishes, and the values it uses.
///
/// Every type here mirrors one in the server's `internal/api/views`, field
/// name for field name. They are kept deliberately dumb — parsing and
/// nothing else — so that a change to the server's contract shows up as a
/// change in this one file.
library;

/// ChatStatus : Where a chat has got to.
///
/// The server's `internal/chat` declares these; a value it does not know is
/// kept as [unknown] rather than throwing, so that a server which learns a
/// new status does not break an older client outright.
enum ChatStatus {
  pending('pending'),
  running('running'),
  completed('completed'),
  failed('failed'),
  cancelled('cancelled'),
  unknown('');

  const ChatStatus(this.wire);

  /// wire : How the status is written in JSON.
  final String wire;

  /// isTerminal : Whether the chat has finished, however it finished.
  bool get isTerminal =>
      this == completed || this == failed || this == cancelled;

  /// parse : Returns the status named by [value], or [unknown].
  static ChatStatus parse(String? value) => ChatStatus.values.firstWhere(
    (s) => s.wire == value,
    orElse: () => ChatStatus.unknown,
  );
}

/// EventKind : What a streamed event is.
///
/// The first is progress; the rest each end the stream.
enum EventKind {
  update('update'),
  finalAnswer('final'),
  error('error'),
  cancelled('cancelled'),
  unknown('');

  const EventKind(this.wire);

  /// wire : How the kind is written in JSON and in the SSE event field.
  final String wire;

  /// isTerminal : Whether an event of this kind ends the stream.
  ///
  /// An unknown kind is not treated as terminal: a client that hung up on a
  /// kind it did not recognise would miss the answer that followed.
  bool get isTerminal =>
      this == finalAnswer || this == error || this == cancelled;

  /// parse : Returns the kind named by [value], or [unknown].
  static EventKind parse(String? value) => EventKind.values.firstWhere(
    (k) => k.wire == value,
    orElse: () => EventKind.unknown,
  );
}

/// Chat : One prompt and, once there is one, its answer.
class Chat {
  const Chat({
    required this.id,
    required this.sessionId,
    required this.prompt,
    required this.status,
    required this.response,
    required this.error,
    required this.createdAt,
    required this.updatedAt,
    this.startedAt,
    this.finishedAt,
  });

  final String id;
  final String sessionId;
  final String prompt;
  final ChatStatus status;

  /// response : The answer. Empty until the chat completes.
  final String response;

  /// error : Why it failed, phrased to be spoken. Empty unless it did.
  final String error;

  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? startedAt;
  final DateTime? finishedAt;

  /// fromJson : Parses a chat as the API returns it.
  factory Chat.fromJson(Map<String, dynamic> json) => Chat(
    id: json['id'] as String,
    sessionId: json['session_id'] as String? ?? '',
    prompt: json['prompt'] as String? ?? '',
    status: ChatStatus.parse(json['status'] as String?),
    response: json['response'] as String? ?? '',
    error: json['error'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
    updatedAt: _time(json['updated_at'])!,
    startedAt: _time(json['started_at']),
    finishedAt: _time(json['finished_at']),
  );

  @override
  String toString() => 'Chat($id, ${status.wire})';
}

/// ChatSummary : A chat in a listing, which carries no response body.
class ChatSummary {
  const ChatSummary({
    required this.id,
    required this.sessionId,
    required this.prompt,
    required this.status,
    required this.error,
    required this.createdAt,
    required this.updatedAt,
    this.startedAt,
    this.finishedAt,
  });

  final String id;
  final String sessionId;
  final String prompt;
  final ChatStatus status;
  final String error;
  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? startedAt;
  final DateTime? finishedAt;

  /// fromJson : Parses a chat summary as the API returns it.
  factory ChatSummary.fromJson(Map<String, dynamic> json) => ChatSummary(
    id: json['id'] as String,
    sessionId: json['session_id'] as String? ?? '',
    prompt: json['prompt'] as String? ?? '',
    status: ChatStatus.parse(json['status'] as String?),
    error: json['error'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
    updatedAt: _time(json['updated_at'])!,
    startedAt: _time(json['started_at']),
    finishedAt: _time(json['finished_at']),
  );

  @override
  String toString() => 'ChatSummary($id, ${status.wire})';
}

/// Message : One thing a chat said while it ran.
class Message {
  const Message({
    required this.seq,
    required this.kind,
    required this.text,
    required this.createdAt,
  });

  /// seq : Its position in the chat's messages, counting from one. This is
  /// what a reconnecting client resumes from.
  final int seq;

  final EventKind kind;

  /// text : What to say. Written to be spoken aloud.
  final String text;

  final DateTime createdAt;

  /// fromJson : Parses a message as the API returns it.
  factory Message.fromJson(Map<String, dynamic> json) => Message(
    seq: json['seq'] as int? ?? 0,
    kind: EventKind.parse(json['kind'] as String?),
    text: json['text'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
  );

  @override
  String toString() => 'Message($seq, ${kind.wire})';
}

/// User : The account everything belongs to.
class User {
  const User({
    required this.id,
    required this.username,
    required this.createdAt,
  });

  final String id;
  final String username;
  final DateTime createdAt;

  /// fromJson : Parses a user as the API returns it.
  factory User.fromJson(Map<String, dynamic> json) => User(
    id: json['id'] as String,
    username: json['username'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
  );

  @override
  String toString() => 'User($id, $username)';
}

/// Client : One place the user talks to the assistant from, holding one token.
class Client {
  const Client({
    required this.id,
    required this.name,
    required this.current,
    required this.revoked,
    required this.activeSessionId,
    required this.createdAt,
    this.revokedAt,
  });

  final String id;
  final String name;

  /// current : Whether this is the client the request was made from.
  final bool current;

  final bool revoked;
  final DateTime? revokedAt;

  /// activeSessionId : Where a prompt from this client lands.
  final String activeSessionId;

  final DateTime createdAt;

  /// fromJson : Parses a client as the API returns it.
  factory Client.fromJson(Map<String, dynamic> json) => Client(
    id: json['id'] as String,
    name: json['name'] as String? ?? '',
    current: json['current'] as bool? ?? false,
    revoked: json['revoked'] as bool? ?? false,
    revokedAt: _time(json['revoked_at']),
    activeSessionId: json['active_session_id'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
  );

  @override
  String toString() => 'Client($id, $name)';
}

/// Session : One thread of conversation, owned by the user rather than by any
/// one of their clients.
class Session {
  const Session({
    required this.id,
    required this.title,
    required this.active,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String title;

  /// active : Whether this is where the calling client's prompts land.
  final bool active;

  final DateTime createdAt;
  final DateTime updatedAt;

  /// fromJson : Parses a session as the API returns it.
  factory Session.fromJson(Map<String, dynamic> json) => Session(
    id: json['id'] as String,
    title: json['title'] as String? ?? '',
    active: json['active'] as bool? ?? false,
    createdAt: _time(json['created_at'])!,
    updatedAt: _time(json['updated_at'])!,
  );

  @override
  String toString() => 'Session($id, $title)';
}

/// SessionDetail : A session together with its chats, oldest first.
class SessionDetail {
  const SessionDetail({required this.session, required this.chats});

  final Session session;
  final List<ChatSummary> chats;

  /// fromJson : Parses a session detail as the API returns it.
  factory SessionDetail.fromJson(Map<String, dynamic> json) => SessionDetail(
    session: Session.fromJson(json['session'] as Map<String, dynamic>),
    chats: _list(json['chats'], ChatSummary.fromJson),
  );
}

/// Identity : Who is calling and from what.
class Identity {
  const Identity({required this.user, required this.client});

  final User user;
  final Client client;

  /// fromJson : Parses an identity as the API returns it.
  factory Identity.fromJson(Map<String, dynamic> json) => Identity(
    user: User.fromJson(json['user'] as Map<String, dynamic>),
    client: Client.fromJson(json['client'] as Map<String, dynamic>),
  );
}

/// LoginResult : What logging in returns.
///
/// The token appears here and nowhere else. Only its hash is stored on the
/// server, so this is the one chance to keep it.
class LoginResult {
  const LoginResult({
    required this.token,
    required this.user,
    required this.client,
  });

  final String token;
  final User user;
  final Client client;

  /// fromJson : Parses a login response.
  factory LoginResult.fromJson(Map<String, dynamic> json) => LoginResult(
    token: json['token'] as String,
    user: User.fromJson(json['user'] as Map<String, dynamic>),
    client: Client.fromJson(json['client'] as Map<String, dynamic>),
  );

  /// toString : Deliberately omits the token, so that printing a login
  /// result cannot put a credential in a log.
  @override
  String toString() => 'LoginResult(${user.username}, ${client.id})';
}

/// ChatEvent : One event as it arrives on the stream.
class ChatEvent {
  const ChatEvent({
    required this.kind,
    required this.seq,
    required this.text,
    required this.at,
  });

  final EventKind kind;

  /// seq : Its position in the chat's messages, or zero for an event that is
  /// not a stored message. Only a non-zero value is worth resuming from.
  final int seq;

  /// text : What to say.
  final String text;

  final DateTime at;

  /// isTerminal : Whether this event ends the stream.
  bool get isTerminal => kind.isTerminal;

  /// fromJson : Parses the JSON carried in an event's data field.
  factory ChatEvent.fromJson(Map<String, dynamic> json) => ChatEvent(
    kind: EventKind.parse(json['kind'] as String?),
    seq: json['seq'] as int? ?? 0,
    text: json['text'] as String? ?? '',
    at: _time(json['at']) ?? DateTime.now().toUtc(),
  );

  @override
  String toString() => 'ChatEvent(${kind.wire}, $seq)';
}

/// _time : Parses an RFC 3339 timestamp, returning null when absent.
///
/// The server omits a timestamp that has not happened yet rather than sending
/// a zero, so absence is normal and not an error.
DateTime? _time(Object? value) {
  if (value is! String || value.isEmpty) return null;
  return DateTime.parse(value).toUtc();
}

/// _list : Parses a JSON array, tolerating null for an empty one.
List<T> _list<T>(Object? value, T Function(Map<String, dynamic>) parse) {
  if (value is! List) return const [];
  return value.cast<Map<String, dynamic>>().map(parse).toList(growable: false);
}

/// parseList : Parses a named array from a response body.
List<T> parseList<T>(
  Map<String, dynamic> json,
  String field,
  T Function(Map<String, dynamic>) parse,
) => _list(json[field], parse);
