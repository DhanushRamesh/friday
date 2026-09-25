/// Everything the screens read and change, in one place.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../api/client.dart';

/// Turn : One exchange — what was asked, and what came back.
///
/// The server keeps these as a chat, which is a unit of work rather than a
/// unit of conversation: it has a status, a start and a finish. A screen only
/// wants the two halves and whether the second is still arriving.
@immutable
class Turn {
  const Turn({
    required this.chatId,
    required this.prompt,
    required this.answer,
    required this.status,
    this.error = '',
    this.detail = '',
  });

  final String chatId;
  final String prompt;

  /// answer : What has been said so far. Grows while the chat is running.
  final String answer;

  final ChatStatus status;

  /// error : Why it did not answer, when it did not. Shown instead of the
  /// answer rather than beside it.
  final String error;

  /// detail : Exactly what the service said. Kept out of [error] because that
  /// is the sentence a person reads first; this is what they get when they
  /// ask for more.
  final String detail;

  bool get isRunning => !status.isTerminal;

  Turn copyWith({
    String? answer,
    ChatStatus? status,
    String? error,
    String? detail,
  }) => Turn(
    chatId: chatId,
    prompt: prompt,
    answer: answer ?? this.answer,
    status: status ?? this.status,
    error: error ?? this.error,
    detail: detail ?? this.detail,
  );
}

/// AppState : The client's whole state, and the only thing that calls the API.
///
/// Screens read it and call its methods; nothing else talks to [AssistantApi].
/// That keeps the rule about which call follows which — activate before
/// sending, reload after creating — in one file instead of spread across
/// the widgets that happen to trigger it.
class AppState extends ChangeNotifier {
  AppState({required this.api});

  final AssistantApi api;

  /// signedIn : Whether there is a token and an identity behind it.
  bool get signedIn => _identity != null;

  Identity? _identity;
  Identity? get identity => _identity;

  List<Session> _sessions = const [];
  List<Session> get sessions => _sessions;

  String? _sessionId;
  String? get sessionId => _sessionId;

  List<Turn> _turns = const [];
  List<Turn> get turns => _turns;

  List<Client> _clients = const [];
  List<Client> get clients => _clients;

  bool _busy = false;

  /// busy : Whether a whole-screen operation is in flight. Sending is not
  /// one of these: the composer stays usable and the turn shows its own
  /// progress.
  bool get busy => _busy;

  bool _sending = false;
  bool get sending => _sending;

  String? _error;
  String? get error => _error;

  StreamSubscription<ChatEvent>? _stream;

  /// start : Restores a kept token and loads what the screens need, returning
  /// whether there was a usable session.
  ///
  /// A token that the server no longer accepts is discarded here rather than
  /// left to fail the first real call, so the app opens on the login screen
  /// instead of on an empty one that errors.
  Future<bool> start() async {
    if (!await api.restore()) return false;
    try {
      _identity = await api.me();
      await _loadSessions();
      return true;
    } on Object {
      await api.logout();
      _identity = null;
      return false;
    }
  }

  /// signIn : Authenticates, then loads the sessions the token can see.
  Future<bool> signIn({
    required String username,
    required String password,
  }) async {
    _set(busy: true, error: null);
    try {
      final result = await api.login(
        username: username,
        password: password,
        clientName: 'web',
      );
      _identity = Identity(user: result.user, client: result.client);
      await _loadSessions();
      return true;
    } on Object catch (e) {
      _error = _explain(e);
      return false;
    } finally {
      _set(busy: false);
    }
  }

  /// signOut : Forgets the token and everything it was showing.
  Future<void> signOut() async {
    await _stream?.cancel();
    _stream = null;
    await api.logout();
    _identity = null;
    _sessions = const [];
    _clients = const [];
    _turns = const [];
    _sessionId = null;
    _error = null;
    notifyListeners();
  }

  /// newSession : Starts a session and moves into it.
  Future<void> newSession() async {
    _set(busy: true, error: null);
    try {
      final created = await api.createSession();
      await _loadSessions();
      await _open(created.id);
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// select : Switches to a session, making it the one this client is in.
  ///
  /// Activating matters beyond this screen: a chat sent without a session
  /// joins whichever one the client is active in, so the voice satellite and
  /// this client would otherwise drift into different conversations.
  Future<void> select(String id) async {
    if (id == _sessionId) return;
    _set(busy: true, error: null);
    try {
      await api.activateSession(id);
      await _open(id);
      _sessions = [
        for (final s in _sessions) Session(
          id: s.id,
          title: s.title,
          active: s.id == id,
          createdAt: s.createdAt,
          updatedAt: s.updatedAt,
        ),
      ];
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// send : Asks something, and follows the answer as it arrives.
  Future<void> send(String prompt) async {
    final text = prompt.trim();
    if (text.isEmpty || _sending) return;

    _sending = true;
    _error = null;
    notifyListeners();

    try {
      final chat = await api.createChat(text, sessionId: _sessionId);
      _sessionId ??= chat.sessionId;
      _turns = [
        ..._turns,
        Turn(
          chatId: chat.id,
          prompt: text,
          answer: '',
          status: chat.status,
        ),
      ];
      notifyListeners();
      await _follow(chat.id);
      // The title is the first prompt, so a new session only gets a name
      // once something has been asked in it.
      await _loadSessions();
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _sending = false;
      notifyListeners();
    }
  }

  /// cancel : Stops the turn that is still running, if there is one.
  Future<void> cancel() async {
    final running = _turns.where((t) => t.isRunning).toList();
    if (running.isEmpty) return;
    try {
      await api.cancelChat(running.last.chatId);
    } on Object catch (e) {
      _error = _explain(e);
      notifyListeners();
    }
  }

  /// refresh : Reads the sessions and the open transcript again.
  ///
  /// The voice satellite writes into the same sessions this is showing, so
  /// what is on screen goes stale whenever something is said out loud. There
  /// is nothing pushing that here — the only stream the client opens is for a
  /// chat it started itself — so seeing it means asking.
  Future<void> refresh() async {
    if (_busy) return;
    _set(busy: true, error: null);
    try {
      _sessions = await api.listSessions();
      final open = _sessionId;
      if (open != null && _sessions.any((s) => s.id == open)) {
        await _open(open, keepVisible: true);
      }
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// loadClients : Reads the clients holding a token, for the settings screen.
  Future<void> loadClients() async {
    try {
      _clients = await api.listClients();
    } on Object catch (e) {
      _error = _explain(e);
    }
    notifyListeners();
  }

  /// revoke : Takes a client's token away. Revoking this one signs it out,
  /// because the token it is holding stops working the moment the call
  /// returns.
  Future<void> revoke(String clientId) async {
    final self = _identity?.client.id == clientId;
    try {
      await api.revokeClient(clientId);
      if (self) {
        await signOut();
        return;
      }
      await loadClients();
    } on Object catch (e) {
      _error = _explain(e);
      notifyListeners();
    }
  }

  /// dismissError : Clears the banner without retrying anything.
  void dismissError() {
    if (_error == null) return;
    _error = null;
    notifyListeners();
  }

  @override
  void dispose() {
    _stream?.cancel();
    super.dispose();
  }

  /// _follow : Reads a chat's stream into the last turn until it ends.
  ///
  /// Updates replace rather than append: the server sends the answer so far,
  /// not the piece that is new, so concatenating would repeat everything.
  Future<void> _follow(String chatId) async {
    await _stream?.cancel();
    final done = Completer<void>();

    _stream = api.streamChat(chatId).listen(
      (event) {
        switch (event.kind) {
          case EventKind.update:
            _replace(chatId, (t) => t.copyWith(
              answer: event.text,
              status: ChatStatus.running,
            ));
          case EventKind.finalAnswer:
            _replace(chatId, (t) => t.copyWith(
              answer: event.text,
              status: ChatStatus.completed,
            ));
          case EventKind.error:
            _replace(chatId, (t) => t.copyWith(
              status: ChatStatus.failed,
              error: event.text,
              detail: event.detail,
            ));
          case EventKind.cancelled:
            _replace(chatId, (t) => t.copyWith(status: ChatStatus.cancelled));
          case EventKind.unknown:
            break;
        }
      },
      onError: (Object e) {
        _replace(chatId, (t) => t.copyWith(
          status: ChatStatus.failed,
          error: _explain(e),
        ));
        if (!done.isCompleted) done.complete();
      },
      onDone: () {
        if (!done.isCompleted) done.complete();
      },
      cancelOnError: true,
    );

    await done.future;
  }

  /// _open : Loads a session's turns and shows them.
  ///
  /// A listing carries each chat's prompt but not its answer, so the answers
  /// are fetched one call per chat. They go together rather than in turn: a
  /// session of twenty chats would otherwise take twenty round trips end to
  /// end before anything appeared.
  /// [keepVisible] leaves what is on screen in place while the new turns are
  /// fetched, which is what a refresh of the session already open wants:
  /// clearing first would blank a transcript only being brought up to date.
  Future<void> _open(String id, {bool keepVisible = false}) async {
    _sessionId = id;
    if (!keepVisible) _turns = const [];
    notifyListeners();

    final detail = await api.session(id);
    final chats = await Future.wait(
      detail.chats.map((c) async {
        try {
          return await api.chat(c.id);
        } on Object {
          // One unreadable chat should not empty the whole transcript.
          return Chat(
            id: c.id,
            sessionId: c.sessionId,
            prompt: c.prompt,
            status: c.status,
            response: '',
            error: c.error,
            errorCode: c.errorCode,
            createdAt: c.createdAt,
            updatedAt: c.updatedAt,
          );
        }
      }),
    );

    _turns = [
      for (final c in chats)
        Turn(
          chatId: c.id,
          prompt: c.prompt,
          answer: c.response,
          status: c.status,
          error: c.error,
          detail: c.errorDetail,
        ),
    ];
    notifyListeners();
  }

  Future<void> _loadSessions() async {
    _sessions = await api.listSessions();
    final active = _sessions.where((s) => s.active).toList();
    if (_sessionId == null && active.isNotEmpty) {
      await _open(active.first.id);
    }
    notifyListeners();
  }

  void _replace(String chatId, Turn Function(Turn) change) {
    _turns = [
      for (final t in _turns) if (t.chatId == chatId) change(t) else t,
    ];
    notifyListeners();
  }

  void _set({bool? busy, String? error}) {
    if (busy != null) _busy = busy;
    if (error != null || busy == true) _error = error;
    notifyListeners();
  }

  /// _explain : Turns a failure into a sentence worth showing.
  ///
  /// The API's own exceptions already carry one written for a person; only
  /// anything else needs a fallback.
  static String _explain(Object e) =>
      e is ApiException ? e.message : 'Something went wrong.';
}
