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
    required this.conversationId,
    required this.prompt,
    required this.status,
    required this.response,
    required this.error,
    this.errorCode = '',
    this.errorDetail = '',
    required this.createdAt,
    required this.updatedAt,
    this.startedAt,
    this.finishedAt,
  });

  final String id;
  final String conversationId;
  final String prompt;
  final ChatStatus status;

  /// response : The answer. Empty until the chat completes.
  final String response;

  /// error : Why it failed, phrased to be spoken. Empty unless it did.
  final String error;

  /// errorCode : Which kind of failure it was. Worth keying off rather than
  /// matching on [error], which is prose and will be reworded.
  final String errorCode;

  /// errorDetail : Exactly what the service said. Shown on request, never in
  /// place of [error].
  final String errorDetail;

  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? startedAt;
  final DateTime? finishedAt;

  /// fromJson : Parses a chat as the API returns it.
  factory Chat.fromJson(Map<String, dynamic> json) => Chat(
    id: json['id'] as String,
    conversationId: json['conversation_id'] as String? ?? '',
    prompt: json['prompt'] as String? ?? '',
    status: ChatStatus.parse(json['status'] as String?),
    response: json['response'] as String? ?? '',
    error: json['error'] as String? ?? '',
    errorCode: json['error_code'] as String? ?? '',
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
    required this.conversationId,
    required this.prompt,
    required this.status,
    required this.error,
    this.errorCode = '',
    required this.createdAt,
    required this.updatedAt,
    this.startedAt,
    this.finishedAt,
  });

  final String id;
  final String conversationId;
  final String prompt;
  final ChatStatus status;
  final String error;

  /// errorCode : Which kind of failure it was. A listing carries this but not
  /// the detail, which the server does not select for a list.
  final String errorCode;

  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? startedAt;
  final DateTime? finishedAt;

  /// fromJson : Parses a chat summary as the API returns it.
  factory ChatSummary.fromJson(Map<String, dynamic> json) => ChatSummary(
    id: json['id'] as String,
    conversationId: json['conversation_id'] as String? ?? '',
    prompt: json['prompt'] as String? ?? '',
    status: ChatStatus.parse(json['status'] as String?),
    error: json['error'] as String? ?? '',
    errorCode: json['error_code'] as String? ?? '',
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
    this.channel = '',
    this.vendor = '',
    this.model = '',
    required this.revoked,
    required this.activeConversationId,
    required this.createdAt,
    this.revokedAt,
  });

  final String id;
  final String name;

  /// current : Whether this is the client the request was made from.
  final bool current;

  /// channel : How this client's prompts are treated, "voice" or "direct".
  final String channel;

  /// vendor, model : Which model answers this client. Both empty when it has
  /// chosen none and the server's configured one answers.
  final String vendor;
  final String model;

  final bool revoked;
  final DateTime? revokedAt;

  /// activeConversationId : Where a prompt from this client lands.
  final String activeConversationId;

  final DateTime createdAt;

  /// fromJson : Parses a client as the API returns it.
  factory Client.fromJson(Map<String, dynamic> json) => Client(
    id: json['id'] as String,
    name: json['name'] as String? ?? '',
    current: json['current'] as bool? ?? false,
    channel: json['channel'] as String? ?? '',
    vendor: json['vendor'] as String? ?? '',
    model: json['model'] as String? ?? '',
    revoked: json['revoked'] as bool? ?? false,
    revokedAt: _time(json['revoked_at']),
    activeConversationId: json['active_conversation_id'] as String? ?? '',
    createdAt: _time(json['created_at'])!,
  );

  @override
  String toString() => 'Client($id, $name)';
}

/// Conversation : One thread of conversation, owned by the user rather than by any
/// one of their clients.
class Conversation {
  const Conversation({
    required this.id,
    required this.title,
    required this.active,
    this.archived = false,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String title;

  /// active : Whether this is where the calling client's prompts land.
  final bool active;

  /// archived : Whether it has been put away. An archived conversation keeps
  /// everything said in it and simply stops being offered.
  final bool archived;

  final DateTime createdAt;
  final DateTime updatedAt;

  /// fromJson : Parses a conversation as the API returns it.
  factory Conversation.fromJson(Map<String, dynamic> json) => Conversation(
    id: json['id'] as String,
    title: json['title'] as String? ?? '',
    active: json['active'] as bool? ?? false,
    archived: json['archived'] as bool? ?? false,
    createdAt: _time(json['created_at'])!,
    updatedAt: _time(json['updated_at'])!,
  );

  @override
  String toString() => 'Conversation($id, $title)';
}

/// ConversationDetail : A conversation together with its chats, oldest first.
class ConversationDetail {
  const ConversationDetail({required this.conversation, required this.chats});

  final Conversation conversation;
  final List<ChatSummary> chats;

  /// fromJson : Parses a conversation detail as the API returns it.
  factory ConversationDetail.fromJson(Map<String, dynamic> json) => ConversationDetail(
    conversation: Conversation.fromJson(json['conversation'] as Map<String, dynamic>),
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

/// AnswerChunk : One line of the answer, as the Ollama-shaped endpoint sends
/// it.
///
/// Newline-delimited JSON: each line carries a piece of what to say, and the
/// last carries [done] together with the failure's code and detail when there
/// was one.

class AnswerChunk {
  const AnswerChunk({
    required this.text,
    required this.done,
    this.doneReason = '',
    this.errorCode = '',
    this.errorDetail = '',
  });

  /// text : What to add to the answer so far. Empty on the last line.
  final String text;

  /// done : Whether this is the last line.
  final bool done;

  /// doneReason : Why it ended — "stop", "error" or "cancelled".
  final String doneReason;

  /// errorCode, errorDetail : Set on the last line when the chat failed.
  /// Beyond Ollama's shape, and only a screen can use them.
  final String errorCode;
  final String errorDetail;

  bool get failed => doneReason == 'error';

  factory AnswerChunk.fromJson(Map<String, dynamic> json) {
    final message = json['message'] as Map<String, dynamic>?;
    return AnswerChunk(
      text: (message?['content'] as String?) ?? '',
      done: json['done'] as bool? ?? false,
      doneReason: json['done_reason'] as String? ?? '',
      errorCode: json['error_code'] as String? ?? '',
      errorDetail: json['error_detail'] as String? ?? '',
    );
  }

  @override
  String toString() => 'AnswerChunk(done: $done)';
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

/// LlmModel : A model a client can be set to answer with.
///
/// Named so as not to collide with the word this file uses for everything it
/// parses. It is a model in the other sense.
class LlmModel {
  const LlmModel({
    required this.id,
    required this.name,
    required this.vendor,
    required this.contextTokens,
    required this.supportsTools,
  });

  final String id;
  final String name;
  final String vendor;

  /// contextTokens : How much the model can be given at once. Shown so the
  /// choice between a large model and a fast one is an informed one.
  final int contextTokens;

  final bool supportsTools;

  /// fromJson : Parses a model as the API returns it.
  factory LlmModel.fromJson(Map<String, dynamic> json) => LlmModel(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? '',
    vendor: json['vendor'] as String? ?? '',
    contextTokens: json['context_tokens'] as int? ?? 0,
    supportsTools: json['supports_tools'] as bool? ?? false,
  );

  @override
  String toString() => 'LlmModel($vendor/$id)';
}

/// ModelCatalogue : What a client may be answered by, and what answers it when
/// it has chosen nothing.
class ModelCatalogue {
  const ModelCatalogue({this.models = const [], this.defaultId = ''});

  final List<LlmModel> models;

  /// defaultId : The model answering a client that has chosen none. Empty
  /// when the server did not say.
  final String defaultId;

  /// defaultName : What to call that model, falling back to its identifier
  /// and then to nothing worth naming.
  String get defaultName {
    for (final m in models) {
      if (m.id == defaultId) return m.name;
    }
    return defaultId;
  }

  /// fromJson : Parses the listing as the API returns it.
  factory ModelCatalogue.fromJson(Map<String, dynamic> json) => ModelCatalogue(
    models: [
      for (final m in (json['models'] as List<dynamic>? ?? const []))
        LlmModel.fromJson(m as Map<String, dynamic>),
    ],
    defaultId: json['default'] as String? ?? '',
  );
}

/// PersonaOption : A manner the assistant can answer in.
class PersonaOption {
  const PersonaOption({
    required this.id,
    required this.name,
    required this.summary,
  });

  final String id;
  final String name;
  final String summary;

  /// fromJson : Parses a persona as the API returns it.
  factory PersonaOption.fromJson(Map<String, dynamic> json) => PersonaOption(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? '',
    summary: json['summary'] as String? ?? '',
  );
}

/// Personas : The manners on offer and the one in use.
class Personas {
  const Personas({this.options = const [], this.current = ''});

  final List<PersonaOption> options;

  /// current : The identifier of the manner in use.
  final String current;

  /// currentName : What to call it, falling back to nothing worth naming.
  String get currentName {
    for (final p in options) {
      if (p.id == current) return p.name;
    }
    return current;
  }

  /// fromJson : Parses the listing as the API returns it.
  factory Personas.fromJson(Map<String, dynamic> json) => Personas(
    options: [
      for (final p in (json['personas'] as List<dynamic>? ?? const []))
        PersonaOption.fromJson(p as Map<String, dynamic>),
    ],
    current: json['current'] as String? ?? '',
  );
}
