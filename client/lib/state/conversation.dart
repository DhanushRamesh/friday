/// The state of one session's conversation.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import '../friday/friday.dart';

/// _resumeAttempts : How many times a dropped stream is reopened before
/// giving up and falling back to asking how the chat ended.
///
/// A phone loses its connection often, and the chat keeps running on the
/// server regardless, so reconnecting is the normal case rather than an
/// error.
const int _resumeAttempts = 4;

/// _resumeBackoff : How long to wait before each reopen.
const List<Duration> _resumeBackoff = [
  Duration(milliseconds: 400),
  Duration(seconds: 1),
  Duration(seconds: 2),
  Duration(seconds: 4),
];

/// VoiceCue : Something for the voice layer to do.
///
/// The conversation says what should be heard; it does not know whether
/// anything is listening or how it would be said. That keeps the audio out
/// of the state and lets both be tested apart.
sealed class VoiceCue {
  const VoiceCue();
}

/// SpeakCue : Say this.
class SpeakCue extends VoiceCue {
  const SpeakCue(this.text, {this.urgent = false});

  final String text;

  /// urgent : Whether it replaces what is queued rather than joining it. A
  /// failure is urgent: following it with the progress that preceded it
  /// would be reading out something no longer true.
  final bool urgent;
}

/// HushCue : Stop talking and forget the rest.
///
/// What superseding and stopping produce. Continuing to read an answer the
/// user has already talked over is the worst thing a voice assistant does.
class HushCue extends VoiceCue {
  const HushCue();
}

/// Exchange : One prompt and everything that came back for it.
///
/// The unit the conversation is drawn from. It exists while the prompt is
/// still only local, before the server has given it an identifier, which is
/// what lets the prompt appear the instant it is sent.
class Exchange {
  Exchange({
    required this.localId,
    required this.prompt,
    this.chatId,
    this.status = ChatStatus.pending,
  });

  /// localId : Identifies the exchange before — and after — the server gives
  /// it a chat identifier, so the widget keeps its place in the list and
  /// does not flicker when the identifier arrives.
  final String localId;

  final String prompt;

  /// chatId : Null until the server has accepted the prompt.
  String? chatId;

  ChatStatus status;

  /// updates : What FRIDAY said while working, in order.
  final List<String> updates = [];

  /// answer : The final answer, once there is one.
  String answer = '';

  /// failure : Why it failed, phrased to be spoken.
  String failure = '';

  /// lastSeq : The highest sequence number heard, so a dropped stream
  /// resumes rather than repeating itself.
  int lastSeq = 0;

  /// sending : Whether the prompt has not yet been accepted by the server.
  bool sending = true;

  /// rejected : Whether the prompt never reached the server at all. Distinct
  /// from a chat that failed: this one can be retried as-is.
  bool rejected = false;

  /// isRunning : Whether FRIDAY is still working on this.
  bool get isRunning => !status.isTerminal && !rejected;

  /// isSuperseded : Whether a later prompt cancelled this one before it
  /// answered. Shown struck through rather than removed, so the
  /// conversation still reads in the order it happened.
  bool get isSuperseded =>
      status == ChatStatus.cancelled && answer.isEmpty && failure.isEmpty;

  /// hasSaidNothing : Whether there is nothing yet to show for it, which is
  /// when the thinking indicator belongs.
  bool get hasSaidNothing =>
      updates.isEmpty && answer.isEmpty && failure.isEmpty;
}

/// Conversation : Holds one session's exchanges and keeps them up to date.
///
/// Everything asynchronous lives here rather than in the widget: sending,
/// streaming, resuming a dropped connection, and reattaching to a chat that
/// was already running when the screen opened. The widget only draws what
/// this exposes.
class Conversation extends ChangeNotifier {
  Conversation({required this.api});

  final FridayApi api;

  /// sessionId : The session being shown. Empty until loaded.
  String sessionId = '';

  /// exchanges : Oldest first, the order the conversation happened in.
  final List<Exchange> exchanges = [];

  /// loading : Whether the history is being fetched.
  bool loading = false;

  /// error : A failure about the conversation as a whole, such as being
  /// unable to load it. A failure of one prompt lives on its exchange.
  String? error;

  /// _streams : The open subscription per exchange, so switching session or
  /// leaving the screen stops them.
  final Map<String, StreamSubscription<ChatEvent>> _streams = {};

  /// _cues : What the voice layer should do, as it becomes true.
  final StreamController<VoiceCue> _cues =
      StreamController<VoiceCue>.broadcast();

  /// cues : Things to say, and moments to stop saying them.
  Stream<VoiceCue> get cues => _cues.stream;

  bool _disposed = false;
  int _localCounter = 0;

  /// running : The exchange FRIDAY is currently working on, if any.
  Exchange? get running {
    for (final e in exchanges.reversed) {
      if (e.isRunning) return e;
    }
    return null;
  }

  /// isIdle : Whether nothing is being worked on, which is when the composer
  /// is at rest.
  bool get isIdle => running == null;

  /// load : Replaces what is shown with the given session's history.
  ///
  /// A chat still running when this is called is reattached to rather than
  /// shown frozen, which is what makes a page refresh mid-answer harmless.
  Future<void> load(String id) async {
    _cancelStreams();
    sessionId = id;
    exchanges.clear();
    loading = true;
    error = null;
    _notify();

    try {
      final detail = await api.session(id);
      for (final summary in detail.chats) {
        exchanges.add(await _restore(summary));
      }
      loading = false;
      _notify();

      // Done after the first paint, so history appears at once rather than
      // waiting on a request per unfinished chat.
      for (final exchange in exchanges) {
        if (exchange.isRunning) _listen(exchange);
      }
    } on FridayException catch (e) {
      loading = false;
      error = e.message;
      _notify();
    }
  }

  /// _restore : Rebuilds an exchange from a stored chat.
  Future<Exchange> _restore(ChatSummary summary) async {
    final exchange = Exchange(
      localId: summary.id,
      prompt: summary.prompt,
      chatId: summary.id,
      status: summary.status,
    )..sending = false;

    // A listing carries no response body, so a finished chat needs reading
    // in full to recover what it said.
    if (summary.status.isTerminal) {
      try {
        final messages = await api.messages(summary.id);
        for (final m in messages) {
          if (m.kind == EventKind.update) exchange.updates.add(m.text);
          exchange.lastSeq = m.seq > exchange.lastSeq
              ? m.seq
              : exchange.lastSeq;
        }
        if (summary.status == ChatStatus.completed) {
          exchange.answer = (await api.chat(summary.id)).response;
        } else if (summary.status == ChatStatus.failed) {
          exchange.failure = summary.error;
        }
      } on FridayException {
        // The exchange is still worth showing without its detail; the prompt
        // and the outcome are already known.
      }
    }
    return exchange;
  }

  /// send : Sends a prompt and starts following the answer.
  ///
  /// The prompt appears before the request is made, because the user has
  /// already committed to it and waiting for a round trip to redraw makes
  /// the interface feel slower than the network actually is.
  Future<void> send(String prompt) async {
    final text = prompt.trim();
    if (text.isEmpty) return;

    // Speaking again supersedes what is still running. The server does this
    // too; doing it here as well means the old exchange dims immediately
    // rather than when its stream catches up.
    debugPrint('FRIDAY: sending "${_short(text)}"');

    final previous = running;
    if (previous != null && previous.chatId != null) {
      debugPrint(
        'FRIDAY: superseding "${_short(previous.prompt)}" — it was still '
        'running, so its answer is cancelled and FRIDAY stops talking',
      );
      previous.status = ChatStatus.cancelled;
      // Talking over FRIDAY means it should stop talking.
      _cue(const HushCue());
    }

    final exchange = Exchange(
      localId: 'local-${_localCounter++}',
      prompt: text,
    );
    exchanges.add(exchange);
    _notify();

    try {
      final chat = await api.createChat(
        text,
        sessionId: sessionId.isEmpty ? null : sessionId,
      );
      exchange
        ..chatId = chat.id
        ..status = chat.status
        ..sending = false;
      // A prompt sent with no session lands in whichever one this client is
      // in, and the reply says which — so the conversation learns where it
      // is from the first answer.
      if (sessionId.isEmpty) sessionId = chat.sessionId;
      _notify();

      if (chat.status.isTerminal) {
        await _settle(exchange);
      } else {
        _listen(exchange);
      }
    } on FridayException catch (e) {
      exchange
        ..sending = false
        ..rejected = true
        ..failure = e.message;
      _notify();
    }
  }

  /// retry : Sends a prompt again that never reached the server.
  Future<void> retry(Exchange exchange) async {
    if (!exchange.rejected) return;
    exchanges.remove(exchange);
    _notify();
    await send(exchange.prompt);
  }

  /// stop : Cancels what FRIDAY is working on. This is what "stop" does
  /// while it is still speaking.
  Future<void> stop() async {
    final exchange = running;
    final id = exchange?.chatId;
    if (exchange == null || id == null) return;

    // Marked at once: the request takes a moment and the user has already
    // decided.
    exchange.status = ChatStatus.cancelled;
    _cue(const HushCue());
    _notify();

    try {
      await api.cancelChat(id);
    } on Conflict {
      // It finished while the request was in flight, which the stream will
      // report.
    } on FridayException {
      // The stream remains the source of truth for how it actually ended.
    }
  }

  /// _listen : Follows a chat's stream, reopening it if it drops.
  void _listen(Exchange exchange, {int attempt = 0}) {
    final id = exchange.chatId;
    if (id == null || _disposed) return;

    _streams[exchange.localId]?.cancel();
    _streams[exchange.localId] = api
        .streamChat(id, resumeFrom: exchange.lastSeq)
        .listen(
          (event) => _apply(exchange, event),
          onError: (Object e) => _streamFailed(exchange, e, attempt),
          onDone: () => _streamEnded(exchange),
          cancelOnError: true,
        );
  }

  /// _apply : Folds one event into the exchange.
  void _apply(Exchange exchange, ChatEvent event) {
    if (event.seq > exchange.lastSeq) exchange.lastSeq = event.seq;

    switch (event.kind) {
      case EventKind.update:
        exchange.updates.add(event.text);
        exchange.status = ChatStatus.running;
        _cue(SpeakCue(event.text));
      case EventKind.finalAnswer:
        exchange.answer = event.text;
        exchange.status = ChatStatus.completed;
        _cue(SpeakCue(event.text));
      case EventKind.error:
        exchange.failure = event.text;
        exchange.status = ChatStatus.failed;
        _cue(SpeakCue(event.text, urgent: true));
      case EventKind.cancelled:
        exchange.status = ChatStatus.cancelled;
        _cue(const HushCue());
      case EventKind.unknown:
        // Something this client does not know about. Ignored rather than
        // shown, and deliberately not treated as the end of the stream.
        return;
    }
    _notify();
  }

  /// _streamFailed : Reopens a dropped stream, or gives up and asks the
  /// server how the chat ended.
  void _streamFailed(Exchange exchange, Object failure, int attempt) {
    if (_disposed) return;

    // A refusal will be refused again; only a transport failure is worth
    // retrying.
    final worthRetrying = failure is Unreachable && attempt < _resumeAttempts;
    if (!worthRetrying) {
      unawaited(_settle(exchange));
      return;
    }

    Timer(_resumeBackoff[attempt], () {
      if (_disposed || !exchange.isRunning) return;
      _listen(exchange, attempt: attempt + 1);
    });
  }

  /// _streamEnded : Handles a stream closing.
  ///
  /// It closes on a terminal event, which needs nothing more. Closing
  /// without one means the connection went rather than the chat, so the
  /// outcome has to be asked for.
  void _streamEnded(Exchange exchange) {
    _streams.remove(exchange.localId);
    if (_disposed || !exchange.isRunning) return;
    unawaited(_settle(exchange));
  }

  /// _settle : Asks the server how a chat ended, for when the stream could
  /// not say.
  Future<void> _settle(Exchange exchange) async {
    final id = exchange.chatId;
    if (id == null || _disposed) return;

    try {
      final chat = await api.chat(id);
      exchange.status = chat.status;
      if (chat.response.isNotEmpty) exchange.answer = chat.response;
      if (chat.error.isNotEmpty) exchange.failure = chat.error;

      // Still going, so the stream dropping was the connection's doing.
      if (!chat.status.isTerminal) {
        _listen(exchange);
        return;
      }
    } on FridayException catch (e) {
      exchange
        ..status = ChatStatus.failed
        ..failure = e.message;
    }
    _notify();
  }

  /// _cancelStreams : Stops following everything.
  void _cancelStreams() {
    for (final sub in _streams.values) {
      sub.cancel();
    }
    _streams.clear();
  }

  /// _cue : Tells the voice layer what to do, if anything is listening.
  void _cue(VoiceCue cue) {
    if (!_disposed && _cues.hasListener) _cues.add(cue);
  }

  /// _notify : Tells listeners, unless this has been disposed — a stream
  /// event can arrive after the screen has gone.
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _cancelStreams();
    unawaited(_cues.close());
    super.dispose();
  }
}

/// _short : A line of text, cut down for a log.
String _short(String text) =>
    text.length <= 40 ? text : '${text.substring(0, 40)}…';
