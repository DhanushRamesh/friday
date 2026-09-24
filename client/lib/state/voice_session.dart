/// Binding a conversation to a voice.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../voice/voice.dart';
import 'conversation.dart';
import 'heard_text.dart';

/// VoiceSession : Speaks what a conversation says, and turns speech back
/// into prompts.
///
/// The one place that knows about both. The conversation says what should
/// be heard without knowing whether anything can speak; the speaker and the
/// ears know nothing about chats. This joins them, so either can be
/// replaced or switched off without the other noticing.
/// _silenceAfterFinished : How long to wait once an engine that endpoints
/// for itself says the utterance is finished.
///
/// Nothing. The reason it was ever more than nothing was that the engine
/// might be wrong — but Endpointer is not guessing, it has measured three
/// seconds of silence, and the microphone it measured never stopped. Waiting
/// again would be counting the same pause twice, which is exactly the bug
/// this used to be.
///
/// An engine that only *thinks* the utterance ended is a different matter:
/// see [_silenceAfterAGuess].
const Duration _silenceAfterFinished = Duration.zero;

/// _silenceAfterAGuess : The same wait for an engine that only thinks the
/// utterance is finished.
///
/// Android and Chrome both call a result final early — a filler, or a second
/// of thought, and they declare the sentence over. Seven hundred milliseconds
/// on top of that cut people off mid-question: "Friday is about" was sent as
/// a whole thought. This is the room to carry on.
const Duration _silenceAfterAGuess = Duration(milliseconds: 1800);

/// _silenceMidSentence : The wait while the recogniser still considers
/// the utterance unfinished. Room to draw breath and carry on.
const Duration _silenceMidSentence = Duration(milliseconds: 3000);

/// _complainAfter : How many failures in a row before the user is told.
///
/// Always awake, one or two failures a minute are ordinary — the engine is
/// still letting go of the last session. Saying so every time would be
/// noise. Saying nothing ever would hide a microphone that has genuinely
/// stopped working.
const int _complainAfter = 4;

/// _retryOpenAfter : How long to wait before trying again, and the step by
/// which that grows.
const Duration _retryOpenAfter = Duration(seconds: 1);

/// _retryOpenAtMost : The longest gap between attempts.
///
/// Retrying for ever at one second would keep a phone that has lost its
/// microphone busy all night for nothing. Backing off to ten seconds costs
/// at most a few seconds of deafness when whatever was wrong clears, and it
/// still clears by itself without anybody touching the phone.
const Duration _retryOpenAtMost = Duration(seconds: 10);

/// _backoffFor : How long to wait before the nth attempt.
Duration _backoffFor(int failures) {
  final grown = _retryOpenAfter * failures;
  return grown > _retryOpenAtMost ? _retryOpenAtMost : grown;
}

// How long to leave the recogniser alone between one question and waiting
// for the next is the engine's own business — see Ears.settleBeforeReopen.
// It was a constant here, one number for every engine, and the number that
// suited a browser was too short for Android: sessions begun that quickly
// are refused, and the refusals looked like a broken microphone.

/// _maxTurn : The longest a single turn may run, however much is said.
///
/// A microphone that never closed would listen to the room indefinitely.
const Duration _maxTurn = Duration(minutes: 2);

/// Chime : The sound that says the name was heard.
typedef Chime = Future<void> Function();

/// _systemChime : A short sound and a tap, using nothing but Flutter.
///
/// The sound is best effort — Android decides what an alert sounds like and
/// on some builds it is nothing at all — so a haptic goes with it. Between
/// them something registers on any phone, without a package or an asset.
Future<void> _systemChime() async {
  // Neither is essential and either can be missing — a browser has no
  // haptics, a desktop no alert sound, and a test no platform at all. A
  // failure to acknowledge is not a reason to stop listening.
  try {
    await SystemSound.play(SystemSoundType.alert);
  } on Object catch (e) {
    debugPrint('FRIDAY: no chime available: $e');
  }
  try {
    await HapticFeedback.mediumImpact();
  } on Object {
    // Nothing to feel. The sound, if there was one, was the point.
  }
}

class VoiceSession extends ChangeNotifier {
  VoiceSession({
    required this._conversation,
    required this._speaker,
    required this._ears,
    WakeRule? wakeRule,
    Chime? chime,
    StayingAwake? stayingAwake,
  }) : _wakeRule = wakeRule ?? NameSpoken(),
       _chime = chime ?? _systemChime,
       _stayingAwake = stayingAwake ?? defaultStayingAwake() {
    _cues = _conversation.cues.listen(_onCue);
    _speaker.addListener(_speakerChanged);
  }

  final Conversation _conversation;
  final Speaker _speaker;
  final Ears _ears;
  final WakeRule _wakeRule;
  final Chime _chime;

  /// _stayingAwake : Whatever the platform needs held so that listening
  /// survives the screen going off. Nothing, everywhere but Android.
  final StayingAwake _stayingAwake;

  /// _chimed : Whether this turn has already acknowledged the name.
  ///
  /// Once per turn. The name is matched against every partial result, of
  /// which there are several a second, and a phone that pinged at each of
  /// them would be worse than one that never pinged at all.
  bool _chimed = false;

  /// awake : Whether the microphone stays open, answering only what is
  /// addressed to FRIDAY by name.
  ///
  /// Off by default: an always-open microphone is something the user turns
  /// on deliberately, not something they discover.
  bool awake = false;

  /// _startedAt : When the current turn began, for the overall cap.
  DateTime? _startedAt;

  /// _wasSpeaking : Whether FRIDAY was talking when last checked, so the
  /// moment it stops can be noticed.
  bool _wasSpeaking = false;

  /// _interrupted : Whether the user spoke over the answer, so what the
  /// microphone holds is theirs and must not be thrown away with the
  /// echo.
  bool _interrupted = false;

  /// _silence : Ends the turn once nothing new has been heard for
  /// the silence budget. Restarted on every new word.
  Timer? _silence;

  /// _openFailures : Consecutive failures to open the microphone, reset
  /// by the first success.
  int _openFailures = 0;

  late final StreamSubscription<VoiceCue> _cues;

  ListeningState _state = ListeningState.off;

  /// _committed : What earlier segments of this turn heard.
  ///
  /// A browser's recogniser hands back one segment at a time and starts
  /// over on each restart, so what was said before a pause has to be kept
  /// here or it is lost.
  String _committed = '';

  /// _segment : What the current segment has heard.
  String _segment = '';

  /// heard : Everything picked up during this turn, shown while listening
  /// so the user can see they are being understood.
  String get heard => joinHeard(_committed, _segment);

  /// _wantListening : Whether the user still means to be listened to.
  ///
  /// The engine ending is not the user finishing. Chrome's recogniser stops
  /// on its own after a short silence — mid-sentence, while someone is
  /// thinking — and treating that as the end of the question sends half of
  /// it. This says whether to start it again instead.
  bool _wantListening = false;

  /// _problem : Why listening is not working, if it is not.
  String? _problem;

  /// problem : What is stopping the voice working — listening or
  /// speaking, whichever has something to report. A synthesiser that
  /// refuses in silence is indistinguishable from one working with the
  /// volume down, so what it says is shown.
  String? get problem => _problem ?? _speaker.problem;

  set problem(String? value) => _problem = value;

  /// _turn : Counts stretches of listening.
  ///
  /// A recogniser does not stop cleanly: speech_to_text reports both
  /// `notListening` and `done`, and can deliver a result between them. A
  /// guard that only cleared what was heard was not enough — the late
  /// result put it back, and the second stop sent the same words a second
  /// time, which the server then treated as the user correcting
  /// themselves and cancelled the first answer mid-sentence. Numbering the
  /// turns means anything arriving from a turn already finished is
  /// ignored outright.
  int _turn = 0;

  bool _disposed = false;

  /// state : What the microphone is doing.
  ListeningState get state => _state;

  /// preparing : How far one-time setup has got, or null when there is none.
  ///
  /// The first time Whisper listens it downloads half a gigabyte of weights,
  /// and `prepare` does not return until it has. Without this the microphone
  /// reads as open for several minutes while hearing nothing, which is
  /// indistinguishable from a fault — and was reported as one.
  ValueListenable<double?> get preparing => _ears.preparing;

  /// listening : Whether the microphone is open.
  bool get listening =>
      _state == ListeningState.listening || _state == ListeningState.starting;

  /// speaking : Whether FRIDAY is talking.
  bool get speaking => _speaker.speaking;

  /// muted : Whether answers are read aloud.
  bool get muted => !_speaker.enabled;

  /// setAwake : Turns always-awake on or off.
  ///
  /// On, the microphone stays open and only what begins with FRIDAY's name
  /// is acted on. Off closes it at once: a user turning the microphone off
  /// means off.
  Future<void> setAwake(bool value) async {
    if (awake == value || _disposed) return;
    awake = value;
    _openFailures = 0;
    _interrupted = false;
    _wasSpeaking = false;
    problem = null;
    _announce();

    if (!value) {
      if (listening) await cancelListening();
      await _stayingAwake.end();
      return;
    }

    // Before opening the microphone, not after: Android will not hand it
    // to a backgrounded app at all, and asking first means the refusal is
    // reported as itself rather than as a microphone that went quiet an
    // hour later for no visible reason.
    // A refusal here is not a reason not to listen. The screen being held
    // awake cannot fail; the foreground service can, and without it FRIDAY
    // still hears everything for as long as it is the app on screen — which
    // on a charger is all the time. Say so and carry on.
    final refused = await _stayingAwake.begin();

    await startListening();

    // Said afterwards, because opening the microphone clears whatever was
    // showing — and only when nothing worse is: a microphone that will not
    // open matters more than a notification that will not appear.
    if (refused != null && problem == null && !_disposed) {
      problem = refused;
      _announce();
    }
  }

  /// toggleMute : Turns speaking on or off, stopping it at once when off.
  Future<void> toggleMute() async {
    await _speaker.setEnabled(!_speaker.enabled);
    notifyListeners();
  }

  /// _speakerChanged : Closes the microphone while FRIDAY is talking, and
  /// opens it again afterwards.
  ///
  /// On a laptop the speaker feeds straight back into the microphone, so
  /// an open microphone transcribes FRIDAY's own answer: words appear in
  /// the box that nobody said, and every one of them restarts the silence
  /// countdown, so the turn never ends.
  ///
  /// The cost is that "FRIDAY, stop" spoken over an answer is not heard —
  /// the stop button remains. Earbuds do not have this problem, the
  /// microphone being at the mouth and the sound in the ear, but there is
  /// no way to know which is in use.
  void _speakerChanged() {
    if (_disposed) return;

    final speaking = _speaker.speaking;
    final stopped = _wasSpeaking && !speaking;
    _wasSpeaking = speaking;

    if (stopped) {
      // Cleared only at the moment it stops. Silencing notifies before
      // the queue has actually drained, so clearing on every
      // notification threw the flag away before the stop it guards.
      final wasInterrupted = _interrupted;
      _interrupted = false;
      if (awake && !wasInterrupted) {
        unawaited(_forgetTheEcho());
        return;
      }
    }
    _announce();
  }

  /// _forgetTheEcho : Starts the microphone over once FRIDAY has finished
  /// talking.
  ///
  /// Everything picked up while it was talking is its own voice. It is
  /// ignored as it arrives, but the recogniser keeps accumulating it, so
  /// after a long answer the transcript holds the whole paragraph. The
  /// next thing the user says is appended to that, and what gets sent is
  /// the answer read back with a question on the end — long enough to be
  /// refused outright, which looks from outside like being ignored.
  ///
  /// Nothing of the user's is lost: an interruption is handled while it
  /// happens, and [_interrupted] keeps this from wiping it.
  Future<void> _forgetTheEcho() async {
    _turn++;
    _wantListening = false;
    _silence?.cancel();
    _silence = null;
    _clearHeard();

    try {
      await _ears.cancel();
    } on Object {
      // Nothing was open, which is the state we wanted.
    }
    _state = ListeningState.off;
    await _reopen();
  }

  /// _onCue : Does what the conversation asked for.
  void _onCue(VoiceCue cue) {
    switch (cue) {
      case SpeakCue(:final text, :final urgent):
        // Held open by the button, the microphone is open only while the
        // user is speaking, so reading an answer over it would mean
        // FRIDAY hearing itself.
        //
        // Always awake the microphone is open all the time, so the same
        // rule would mean never speaking at all. Instead the microphone
        // is closed for as long as FRIDAY talks — see _speakerChanged.
        if (listening && !awake) return;
        _speaker.say(text, interrupts: urgent);
      case HushCue():
        unawaited(_speaker.silence());
    }
  }

  /// startListening : Opens the microphone.
  ///
  /// Silences FRIDAY first: it cannot talk and listen at once without
  /// hearing itself, and the user speaking over an answer means they want
  /// to be heard rather than to keep listening.
  Future<void> startListening({bool hush = true}) async {
    if (listening || _disposed) return;

    // Opening the microphone by hand means the user has stopped listening
    // to the answer. Reopening it between questions, always awake, does
    // not — and silencing there would cut off every answer as it began.
    if (hush) await _speaker.silence();
    _set(ListeningState.starting, heard: '', problem: null);

    final unavailable = await _ears.prepare();
    if (unavailable != null) {
      if (!awake) {
        _set(ListeningState.unavailable, problem: unavailable);
        return;
      }

      // Always awake, keep trying. A recogniser that will not start is
      // usually one that is busy, and a permission that is genuinely
      // refused will keep saying so — visibly, in the banner — rather than
      // silently ending the day's listening.
      _openFailures++;
      _set(ListeningState.starting, problem: unavailable);
      Timer(_backoffFor(_openFailures), () {
        if (!_disposed && awake) unawaited(startListening(hush: false));
      });
      return;
    }
    if (_disposed) return;

    final turn = ++_turn;
    _wantListening = true;
    _startedAt = DateTime.now();
    _committed = '';
    _segment = '';
    _chimed = false;

    // The countdown to the end of a turn starts when something is heard,
    // not when the microphone opens.
    //
    // Started here, always awake, it ends the turn every two seconds in a
    // quiet room and the microphone is stopped and started again — and
    // each cycle is deaf for a moment, so a word spoken into one is
    // simply missed. It feels like having to shout. Held open by the
    // button there is no such loop, and a press with nothing said should
    // still close on its own.
    _silence?.cancel();
    _silence = null;
    if (!awake) {
      _silence = Timer(_silenceMidSentence, () => _silenceElapsed(turn));
    }

    await _openSegment(turn);

    if (_state == ListeningState.starting && !_disposed) {
      _set(ListeningState.listening);
    }
  }

  /// _openSegment : Starts one stretch of recognition within a turn.
  ///
  /// The turn check is repeated here as well as at the caller: a stretch
  /// is opened asynchronously and the turn can finish while that is in
  /// flight, which would leave a recogniser running that nothing is
  /// listening to.
  ///
  /// An engine that refuses is tried once more after being told to stop.
  /// The browser will not start recognition while the previous one is
  /// still running, and it stays running for a moment after the plugin
  /// believes it has stopped — a race hit on every utterance, since
  /// always-awake reopens after each one. An `Ears` is not supposed to
  /// throw at all, but one failure here would end the conversation, so it
  /// is not taken on trust.
  Future<void> _openSegment(int turn, {bool mayRetry = true}) async {
    if (_disposed || turn != _turn) return;

    try {
      await _ears.listen(
        onResult: (result) {
          if (turn != _turn) return;

          if (_speaker.speaking) {
            _heardWhileSpeaking(turn, result.text, settled: result.settled);
            return;
          }

          _openFailures = 0;

          // Recorded before the countdown is armed, not after. With no wait
          // at all the timer can fire on the very next tick, and it used to
          // read a segment that had not been assigned yet — sending an
          // empty question.
          final changed = result.text.trim() != _segment.trim();
          _segment = result.text;
          _acknowledgeTheName();
          if (changed) _noteWords(turn, settled: result.settled);
          if (_state == ListeningState.starting) {
            _state = ListeningState.listening;
          }
          _announce();
        },
        onDone: () => _segmentEnded(turn),
        onError: (message) {
          if (turn != _turn) return;
          _turn++;
          _wantListening = false;
          awake = false;
          _set(ListeningState.unavailable, problem: message);
        },
      );
    } on Object catch (e) {
      if (!mayRetry) {
        debugPrint('FRIDAY: opening the microphone failed: $e');
        _turn++;
        _wantListening = false;

        // Always awake never gives up. The owner's words: "the mic should
        // never stop no mattter what when always awakw option is chosen".
        //
        // A failure to open is almost always the engine still letting go of
        // the last one, and it clears by itself. Switching always-awake off
        // for it means a phone left listening on a charger has quietly
        // stopped being an assistant, and the only way to find out is to
        // ask it something and get nothing. Better to keep trying for ever
        // and say so while it is failing.
        if (awake) {
          _openFailures++;
          _set(
            ListeningState.starting,
            problem: _openFailures >= _complainAfter
                ? 'I am having trouble with the microphone, still trying.'
                : null,
          );
          Timer(_backoffFor(_openFailures), () {
            if (!_disposed && awake) unawaited(_reopen());
          });
          return;
        }

        _openFailures = 0;
        _set(
          ListeningState.unavailable,
          problem: 'I could not open the microphone. Try again.',
        );
        return;
      }
      try {
        await _ears.cancel();
      } on Object {
        // It was not running after all, which is what we wanted.
      }
      await Future<void>.delayed(_ears.settleBeforeReopen);
      await _openSegment(turn, mayRetry: false);
    }
  }

  /// _heardWhileSpeaking : Decides whether something heard during an
  /// answer is the user interrupting or FRIDAY's own voice.
  ///
  /// The microphone stays open while FRIDAY talks, because cancelling or
  /// correcting by voice is the thing a voice assistant most needs to
  /// allow, and closing it would lock the user out for the whole of a
  /// long answer. On a laptop the speaker feeds back into the microphone,
  /// so most of what arrives here is the answer being read back.
  ///
  /// The name separates them, and it is looked for anywhere rather than
  /// at the front: the echo gets there first, so everything before the
  /// name is FRIDAY quoting itself and what follows is the interruption.
  void _heardWhileSpeaking(int turn, String text, {bool settled = false}) {
    // If the answer itself contains the name, an interruption cannot be
    // told from the echo, so none is claimed for this utterance.
    if (_wakeRule.says(_speaker.nowSaying)) return;

    final after = _wakeRule.interruptionIn(text);
    if (after == null) return;

    // Spoken to mid-answer: stop talking and take what follows.
    debugPrint('FRIDAY: interrupted by "$after"');
    _interrupted = true;
    unawaited(_speaker.silence());
    _committed = '';
    _segment = after;
    _noteWords(turn, settled: settled);
    _state = ListeningState.listening;
    _announce();
  }

  /// _noteWords : Records that something new was heard and restarts the
  /// countdown to the end of the turn.
  /// [settled] is the recogniser's own view of whether the utterance is
  /// finished: a short wait when it is, so answering feels prompt, and a
  /// long one while it is not, so a pause for thought is not mistaken for
  /// the end of the question.
  void _noteWords(int turn, {bool settled = false}) {
    _silence?.cancel();
    final Duration wait;
    if (!settled) {
      wait = _silenceMidSentence;
    } else if (_ears.endpointsItself) {
      wait = _silenceAfterFinished;
    } else {
      wait = _silenceAfterAGuess;
    }
    _silence = Timer(wait, () => _silenceElapsed(turn));
  }

  /// _silenceElapsed : Ends the turn because nothing more is being said.
  ///
  /// This, rather than the recogniser stopping, is what decides a turn is
  /// over — so the wait after speaking is the budget alone and not that
  /// plus however long the engine takes to notice.
  void _silenceElapsed(int turn) {
    if (_disposed || turn != _turn || !_wantListening) return;
    unawaited(stopListening());
  }

  /// _acknowledgeTheName : Sounds the chime the first time this turn's
  /// speech contains the name.
  ///
  /// As soon as it is heard rather than when the turn ends, because the
  /// point of it is to tell the speaker to carry on — an acknowledgement
  /// that arrives after the question is finished acknowledges nothing.
  ///
  /// Only when awake. Pressing the button is already an acknowledgement,
  /// and a second one on top of it is noise.
  void _acknowledgeTheName() {
    if (_chimed || !awake || _disposed) return;
    if (!_wakeRule.says(heard)) return;

    _chimed = true;
    unawaited(_chime());
  }

  /// _segmentEnded : Opens another stretch, because the recogniser
  /// stopping is not the user finishing.
  ///
  /// Chrome ends one after any short pause, so a turn must outlive it.
  /// Only the user, the silence timer, or the overall cap ends a turn.
  void _segmentEnded(int turn) {
    if (_disposed || turn != _turn) return;

    // Keep what this segment heard: the next one starts from nothing.
    _committed = heard;
    _segment = '';

    if (!_wantListening) {
      _finishListening(turn);
      return;
    }

    // A microphone that never closed would listen to the room
    // indefinitely.
    final startedAt = _startedAt;
    if (startedAt != null && DateTime.now().difference(startedAt) > _maxTurn) {
      _finishListening(turn);
      return;
    }

    unawaited(_openSegment(turn));
  }

  /// stopListening : Closes the microphone and sends what was heard.
  Future<void> stopListening() async {
    if (!listening) return;
    final turn = _turn;
    // Said before stopping the engine, so the end it reports is understood
    // as the user's doing rather than another pause to restart through.
    _wantListening = false;
    await _ears.stop();
    _finishListening(turn);
  }

  /// cancelListening : Closes the microphone and throws away what was
  /// heard, for when the user changes their mind mid-sentence.
  Future<void> cancelListening() async {
    _turn++;
    _wantListening = false;
    _silence?.cancel();
    _silence = null;
    await _ears.cancel();
    _clearHeard();
    _set(ListeningState.off);
  }

  /// _finishListening : Ends a turn and sends what it heard, once.
  ///
  /// [turn] is which stretch of listening this is the end of. A call for a
  /// turn already finished is ignored, which is what stops one utterance
  /// becoming two prompts.
  ///
  /// A recogniser that heard nothing returns empty, which happens whenever
  /// the microphone opens by accident. Sending that would put an empty
  /// prompt to the model and get a confused answer back.
  void _finishListening(int turn) {
    if (_disposed || turn != _turn) return;
    // Closes this turn, so anything the recogniser reports afterwards
    // belongs to no turn and is dropped.
    _turn++;
    _wantListening = false;
    _silence?.cancel();
    _silence = null;

    final text = heard.trim();
    _clearHeard();

    // Closed. Set without announcing, because when always-awake it is
    // reopened below within the same turn of the event loop and the
    // interface should not flicker between questions.
    _state = ListeningState.off;

    debugPrint(
      'FRIDAY: turn ended, heard "${text.isEmpty ? "(nothing)" : text}"',
    );
    final question = _questionIn(text);

    // Being spoken to means the last answer is no longer wanted. This is
    // how "FRIDAY, stop" works while it is still talking.
    if (question != null) unawaited(_speaker.silence());

    if (awake) {
      unawaited(_reopen());
    } else {
      _announce();
    }
    if (question != null) unawaited(_conversation.send(question));
  }

  /// _questionIn : What to ask, or null when there is nothing to ask.
  ///
  /// Held open, the microphone answers only what is addressed to FRIDAY by
  /// name — everything else is the room talking, and sending it would mean
  /// a nonsense answer and a bill for every overheard sentence. With the
  /// button, pressing it is itself how the user says who they are talking
  /// to, so no name is wanted.
  String? _questionIn(String text) {
    if (text.isEmpty) return null;

    final rule = awake ? _wakeRule : const AlwaysAddressed();
    final addressed = rule.check(text);
    if (!addressed.forFriday) return null;
    return addressed.question.isEmpty ? null : addressed.question;
  }

  /// _reopen : Starts listening again, because always-awake means the
  /// microphone does not close between questions.
  ///
  /// The engine is given a moment first. It is still finishing the
  /// previous stretch when this is reached, and starting a new one on top
  /// of it is refused outright — which, since always-awake reopens after
  /// every utterance, would otherwise happen on every single one.
  Future<void> _reopen() async {
    if (_disposed || !awake) return;
    await Future<void>.delayed(_ears.settleBeforeReopen);
    if (_disposed || !awake || listening) return;
    await startListening(hush: false);
  }

  /// _clearHeard : Forgets this turn's words.
  void _clearHeard() {
    _committed = '';
    _segment = '';
  }

  /// _set : Moves to a state and tells listeners.
  void _set(ListeningState state, {String? heard, String? problem}) {
    _state = state;
    if (heard != null) {
      _committed = heard;
      _segment = '';
    }
    this.problem = problem;
    _announce();
  }

  void _announce() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    awake = false;
    _silence?.cancel();
    unawaited(_cues.cancel());
    _speaker.removeListener(_speakerChanged);
    unawaited(_ears.dispose());
    _speaker.dispose();
    super.dispose();
  }
}
