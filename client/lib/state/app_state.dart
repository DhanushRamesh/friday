/// Everything the screens read and change, in one place.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../api/client.dart';
import '../api/remembered_client.dart';

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
  AppState({required this.api, RememberedClient? remembered})
    : _remembered = remembered ?? RememberedClient();

  final AssistantApi api;

  /// _remembered : What this browser called itself last time. It outlives the
  /// token, so signing out does not turn this into an unknown browser.
  final RememberedClient _remembered;

  String? _clientName;
  String? _clientId;

  /// clientName : The name to sign in under, when one is already known.
  /// Null means this install has never signed in and has to be asked.
  String? get clientName => _clientName;

  /// signedIn : Whether there is a token and an identity behind it.
  bool get signedIn => _identity != null;

  Identity? _identity;
  Identity? get identity => _identity;

  List<Conversation> _conversations = const [];
  List<Conversation> get conversations => _conversations;

  String? _conversationId;
  String? get conversationId => _conversationId;

  List<Turn> _turns = const [];
  List<Turn> get turns => _turns;

  List<Client> _clients = const [];
  List<Client> get clients => _clients;

  Personas _personas = const Personas();

  /// personas : The manners the assistant can answer in, and the one it is
  /// answering in. Empty until the settings screen has read them.
  Personas get personas => _personas;

  ModelCatalogue _catalogue = const ModelCatalogue();

  /// catalogue : What a client can be set to answer with, and which model
  /// answers one that has chosen nothing. Empty until the settings screen
  /// has read it.
  ModelCatalogue get catalogue => _catalogue;

  bool _showRevoked = false;

  /// showRevoked : Whether the clients list is showing what has been revoked
  /// rather than what still works. A revoked client cannot authenticate and
  /// cannot be brought back, so it is a record rather than a row to act on.
  bool get showRevoked => _showRevoked;

  bool _showArchived = false;

  /// showArchived : Whether the sidebar is listing what has been put away
  /// rather than what is in use. The two are separate listings, not one with
  /// a filter, because an archived conversation is never where a prompt lands.
  bool get showArchived => _showArchived;

  bool _busy = false;

  /// busy : Whether a whole-screen operation is in flight. Sending is not
  /// one of these: the composer stays usable and the turn shows its own
  /// progress.
  bool get busy => _busy;

  bool _sending = false;
  bool get sending => _sending;

  String? _error;
  String? get error => _error;


  /// start : Restores a kept token and loads what the screens need, returning
  /// whether there was a usable conversation.
  ///
  /// A token the server refuses is discarded here rather than left to fail
  /// the first real call, so the app opens on the login screen instead of on
  /// an empty one that errors.
  ///
  /// A token it could not ask about is kept. An assistant that is restarting,
  /// unreachable or briefly broken says nothing about whether the token is
  /// still good, and throwing it away means a password has to be typed again
  /// to recover from someone else\'s outage. Reloading once it answers signs
  /// straight back in.
  Future<bool> start() async {
    _clientName = await _remembered.name();
    _clientId = await _remembered.id();
    if (!await api.restore()) {
      notifyListeners();
      return false;
    }
    try {
      _identity = await api.me();
      await _loadConversations();
      return true;
    } on NotAuthenticated {
      await api.logout();
      _identity = null;
      return false;
    } on Object catch (e) {
      _identity = null;
      _error = _explain(e);
      return false;
    }
  }

  /// signIn : Authenticates, then loads the conversations the token can see.
  Future<bool> signIn({
    required String username,
    required String password,
    required String clientName,
  }) async {
    _set(busy: true, error: null);
    try {
      final result = await api.login(
        username: username,
        password: password,
        clientName: clientName,
        clientId: _clientId,
      );
      _identity = Identity(user: result.user, client: result.client);
      // Kept after the server accepted it, and taken from what it registered
      // rather than what was typed: presenting a stale id gets a new client
      // back, and this has to follow that rather than keep pointing at one
      // that no longer exists.
      _clientName = result.client.name.isEmpty ? clientName : result.client.name;
      _clientId = result.client.id;
      await _remembered.remember(id: _clientId!, name: _clientName ?? '');
      await _loadConversations();
      return true;
    } on Object catch (e) {
      _error = _explain(e);
      return false;
    } finally {
      _set(busy: false);
    }
  }

  /// forgetClientName : Lets this browser be named again on the next sign-in.
  Future<void> forgetClientName() async {
    await _remembered.forget();
    _clientName = null;
    _clientId = null;
    notifyListeners();
  }

  /// signOut : Forgets the token and everything it was showing.
  ///
  /// The name this browser registered under is kept. Signing out does not
  /// make this a different browser.
  Future<void> signOut() async {
    await api.logout();
    _identity = null;
    _conversations = const [];
    _clients = const [];
    _turns = const [];
    _conversationId = null;
    _error = null;
    notifyListeners();
  }

  /// newConversation : Starts a conversation and moves into it.
  Future<void> newConversation() async {
    _set(busy: true, error: null);
    try {
      final created = await api.createConversation();
      await _loadConversations();
      await _open(created.id);
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// select : Switches to a conversation, making it the one this client is in.
  ///
  /// Activating matters beyond this screen: a chat sent without a conversation
  /// joins whichever one the client is active in, so the voice satellite and
  /// this client would otherwise drift into different conversations.
  Future<void> select(String id) async {
    if (id == _conversationId) return;
    _set(busy: true, error: null);
    try {
      await api.activateConversation(id);
      await _open(id);
      _conversations = [
        for (final s in _conversations) Conversation(
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

  /// send : Asks something, and reads the answer as it arrives.
  Future<void> send(String prompt) async {
    final text = prompt.trim();
    if (text.isEmpty || _sending) return;

    _sending = true;
    _error = null;
    // Shown at once with an empty answer, so the question appears the moment
    // it is asked rather than when the first word comes back.
    _turns = [
      ..._turns,
      Turn(chatId: '', prompt: text, answer: '', status: ChatStatus.pending),
    ];
    notifyListeners();

    try {
      await _follow(text, _conversationId);
      // The conversation is named after its first prompt, so a new one only gets
      // a name once something has been asked in it.
      _conversations = await api.listConversations(archived: _showArchived);
      await _followTheServer();
    } on Object catch (e) {
      _error = _explain(e);
      _replaceLast((t) => t.copyWith(status: ChatStatus.failed));
    } finally {
      _sending = false;
      notifyListeners();
    }
  }

  /// _followTheServer : Moves to whichever conversation the server now says
  /// is this client's.
  ///
  /// A tool can switch it while answering, and the server is the only thing
  /// that knows: the prompt no longer names a conversation, so what is on
  /// screen would otherwise stay where it was while everything said next
  /// landed somewhere else.
  Future<void> _followTheServer() async {
    for (final c in _conversations) {
      if (!c.active || c.id == _conversationId) continue;
      await _open(c.id);
      return;
    }
  }

  /// cancel : Stops the turn that is still running, if there is one.
  ///
  /// The chat is found rather than remembered: the endpoint answers in
  /// Ollama's shape, which carries no identifier, so the only way to name
  /// what is running is to ask which chat is.
  Future<void> cancel() async {
    if (!_turns.any((t) => t.isRunning)) return;
    try {
      final recent = await api.listChats(limit: 1);
      if (recent.isEmpty || recent.first.status.isTerminal) return;
      await api.cancelChat(recent.first.id);
    } on Object catch (e) {
      _error = _explain(e);
      notifyListeners();
    }
  }

  /// refresh : Reads the conversations and the open transcript again.
  ///
  /// The voice satellite writes into the same conversations this is showing, so
  /// what is on screen goes stale whenever something is said out loud. There
  /// is nothing pushing that here — the only stream the client opens is for a
  /// chat it started itself — so seeing it means asking.
  Future<void> refresh() async {
    if (_busy) return;
    _set(busy: true, error: null);
    try {
      _conversations = await api.listConversations();
      final open = _conversationId;
      if (open != null && _conversations.any((s) => s.id == open)) {
        await _open(open, keepVisible: true);
      }
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// setShowArchived : Switches the sidebar between live and archived.
  Future<void> setShowArchived(bool archived) async {
    if (_showArchived == archived) return;
    _showArchived = archived;
    _set(busy: true, error: null);
    try {
      _conversations = await api.listConversations(archived: archived);
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// archive : Puts a conversation away, or brings it back.
  Future<void> archive(String id, {bool archived = true}) async {
    _set(busy: true, error: null);
    try {
      final active = await api.archiveConversation(id, archived: archived);
      await _afterRemoval(id, active);
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// remove : Deletes a conversation and everything said in it.
  Future<void> remove(String id) async {
    _set(busy: true, error: null);
    try {
      final active = await api.deleteConversation(id);
      await _afterRemoval(id, active);
    } on Object catch (e) {
      _error = _explain(e);
    } finally {
      _set(busy: false);
    }
  }

  /// _afterRemoval : Reloads the listing and opens wherever the client now is.
  ///
  /// The server decides that, because it is the one that knows whether the
  /// conversation removed was the active one. Following its answer rather than
  /// guessing keeps the two from disagreeing about where a prompt will land.
  Future<void> _afterRemoval(String removed, Conversation active) async {
    _conversations = await api.listConversations(archived: _showArchived);
    if (_conversationId == removed) {
      await _open(active.id);
    }
  }

  /// rename : Changes what a conversation is called.
  ///
  /// The listing is patched rather than reloaded: a reload would also reorder
  /// it, and a name changing is not a reason for a conversation to move.
  Future<void> rename(String id, String title) async {
    try {
      final updated = await api.renameConversation(id, title);
      _conversations = [
        for (final s in _conversations)
          if (s.id == id)
            Conversation(
              id: s.id,
              title: updated.title,
              active: s.active,
              createdAt: s.createdAt,
              updatedAt: s.updatedAt,
            )
          else
            s,
      ];
    } on Object catch (e) {
      _error = _explain(e);
    }
    notifyListeners();
  }

  /// loadClients : Reads the clients holding a token, for the settings screen.
  ///
  /// The models are read alongside them, since a client is shown with the one
  /// answering it and there is nothing to choose from without the list.
  Future<void> loadClients() async {
    try {
      _clients = await api.listClients(revoked: _showRevoked);
    } on Object catch (e) {
      _error = _explain(e);
    }
    if (_catalogue.models.isEmpty) {
      try {
        _catalogue = await api.listModels();
      } on Object catch (e) {
        _error = _explain(e);
      }
    }
    if (_personas.options.isEmpty) {
      try {
        _personas = await api.listPersonas();
      } on Object catch (e) {
        _error = _explain(e);
      }
    }
    notifyListeners();
  }

  /// setClientChannel : Changes how a client's prompts are treated.
  Future<void> setClientChannel(String clientId, String channel) async {
    try {
      _clients = await api.setClientChannel(clientId, channel);
    } on Object catch (e) {
      _error = _explain(e);
    }
    notifyListeners();
  }

  /// setClientModel : Chooses which model answers a client's prompts. An
  /// empty [model] puts it back on the server's.
  Future<void> setClientModel(
    String clientId,
    String vendor,
    String model,
  ) async {
    try {
      _clients = await api.setClientModel(clientId, vendor, model);
    } on Object catch (e) {
      _error = _explain(e);
    }
    notifyListeners();
  }

  /// setPersona : Chooses the manner the assistant answers in.
  Future<void> setPersona(String id) async {
    try {
      _personas = await api.setPersona(id);
    } on Object catch (e) {
      _error = _explain(e);
    }
    notifyListeners();
  }

  /// setShowRevoked : Switches the clients list between usable and revoked.
  Future<void> setShowRevoked(bool revoked) async {
    if (_showRevoked == revoked) return;
    _showRevoked = revoked;
    await loadClients();
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
    api.close();
    super.dispose();
  }

  /// _follow : Reads the answer into the last turn until the stream ends.
  ///
  /// Chunks add to what is there rather than replacing it: the endpoint sends
  /// the piece that is new, not the answer so far.
  Future<void> _follow(String prompt, String? conversationId) async {
    var answer = '';
    await for (final piece in api.ask(prompt)) {
      if (piece.text.isNotEmpty) {
        answer += piece.text;
        _replaceLast((t) => t.copyWith(answer: answer, status: ChatStatus.running));
      }
      if (!piece.done) continue;

      switch (piece.doneReason) {
        case 'error':
          _replaceLast((t) => t.copyWith(
            status: ChatStatus.failed,
            error: answer.isEmpty ? 'The service could not complete the request.' : answer,
            detail: piece.errorDetail,
          ));
        case 'cancelled':
          _replaceLast((t) => t.copyWith(status: ChatStatus.cancelled));
        default:
          _replaceLast((t) => t.copyWith(status: ChatStatus.completed));
      }
    }
  }

  /// _open : Loads a conversation's turns and shows them.
  ///
  /// A listing carries each chat's prompt but not its answer, so the answers
  /// are fetched one call per chat. They go together rather than in turn: a
  /// conversation of twenty chats would otherwise take twenty round trips end to
  /// end before anything appeared.
  /// [keepVisible] leaves what is on screen in place while the new turns are
  /// fetched, which is what a refresh of the conversation already open wants:
  /// clearing first would blank a transcript only being brought up to date.
  Future<void> _open(String id, {bool keepVisible = false}) async {
    _conversationId = id;
    if (!keepVisible) _turns = const [];
    notifyListeners();

    final detail = await api.conversation(id);
    final chats = await Future.wait(
      detail.chats.map((c) async {
        try {
          return await api.chat(c.id);
        } on Object {
          // One unreadable chat should not empty the whole transcript.
          return Chat(
            id: c.id,
            conversationId: c.conversationId,
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

  Future<void> _loadConversations() async {
    _conversations = await api.listConversations();
    final active = _conversations.where((s) => s.active).toList();
    if (_conversationId == null && active.isNotEmpty) {
      await _open(active.first.id);
    }
    notifyListeners();
  }

  /// _replaceLast : Rewrites the turn currently being answered.
  ///
  /// By position rather than by identifier: the endpoint answers in Ollama's
  /// shape, which carries no chat id, and the turn being written is always
  /// the one just added.
  void _replaceLast(Turn Function(Turn) change) {
    if (_turns.isEmpty) return;
    final out = [..._turns];
    out[out.length - 1] = change(out.last);
    _turns = out;
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
