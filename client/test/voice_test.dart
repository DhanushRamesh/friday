import 'package:flutter/foundation.dart';
import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/state/conversation.dart';
import 'package:friday_client/state/voice_session.dart';
import 'package:friday_client/voice/voice.dart';

import 'support.dart';

/// RecordingVoice : A synthesiser that records what it was asked to say and
/// lets a test control when each utterance finishes.
class RecordingVoice implements Voice {
  RecordingVoice({this.instant = true});

  /// instant : Whether an utterance completes immediately. False leaves it
  /// hanging until [finish], which is how a test observes the queue.
  final bool instant;

  final List<String> said = [];
  int silences = 0;
  bool disposed = false;

  Completer<void>? _current;

  @override
  Future<bool> available() async => true;

  @override
  Future<void> say(String text) {
    said.add(text);
    if (instant) return Future<void>.value();
    final completer = Completer<void>();
    _current = completer;
    return completer.future;
  }

  /// finish : Ends the utterance being spoken.
  void finish() {
    final current = _current;
    _current = null;
    if (current != null && !current.isCompleted) current.complete();
  }

  @override
  Future<void> silence() async {
    silences++;
    finish();
  }

  @override
  Future<void> dispose() async => disposed = true;
}

/// ScriptedEars : A microphone that says what a test tells it to hear.
class ScriptedEars implements Ears {
  ScriptedEars({
    this.canHear = true,
    this.messyStop = false,
    this.refuseOverlap = false,
    this.accumulates = false,
  });

  /// accumulates : Whether results build up across a session, as a
  /// continuous recogniser's do — each result carries everything heard
  /// so far, not just the newest words. Starting a session clears it.
  ///
  /// Off by default because most tests read better one utterance at a
  /// time, but it is what the browser actually does, and the difference
  /// is exactly what let a whole answer's echo pile up unnoticed.
  final bool accumulates;

  /// _heardSoFar : What this session has accumulated.
  String _heardSoFar = '';

  bool canHear;

  /// refusal : What prepare says when it cannot listen. The reason matters
  /// as much as the fact: see the test about the speech model.
  String refusal = 'I cannot use the microphone. Check the permission.';

  /// refuseOverlap : Whether starting while already running throws, as
  /// the browser's SpeechRecognition does with "recognition has already
  /// started".
  final bool refuseOverlap;

  /// starts : How many times listening was started, and how many of those
  /// were refused.
  int starts = 0;
  int refusals = 0;

  /// messyStop : Whether stopping reports the way speech_to_text really
  /// does — `notListening`, then the final result arriving late, then
  /// `done`. That late result is what turned one spoken question into two
  /// prompts.
  final bool messyStop;

  /// lateResult : What the messy stop delivers after it has already said
  /// it stopped.
  String? lateResult;

  bool listening = false;

  void Function(Heard)? _onResult;
  void Function()? _onDone;

  @override
  Future<String?> prepare() async => canHear ? null : refusal;

  @override
  final ValueListenable<double?> preparing = ValueNotifier<double?>(null);

  @override
  Duration get settleBeforeReopen => Duration.zero;

  /// endpointsItself : Whether this double stands in for an engine that
  /// measured the silence itself, or one that merely guessed.
  ///
  /// A guess by default, because that is what both real platform engines
  /// do and what most of these tests are about. Whisper is the exception
  /// and has a test of its own.
  @override
  bool endpointsItself = false;

  @override
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    if (!canHear) {
      onError('no microphone');
      return;
    }
    starts++;
    if (refuseOverlap && listening) {
      refusals++;
      throw StateError(
        "InvalidStateError: Failed to execute 'start' on "
        "'SpeechRecognition': recognition has already started.",
      );
    }
    listening = true;
    _heardSoFar = '';
    _onResult = onResult;
    _onDone = onDone;
  }

  /// hear : Delivers a partial or settled result.
  void hear(String text, {bool settled = false}) {
    if (!accumulates) {
      _onResult?.call(Heard(text, settled: settled));
      return;
    }
    _heardSoFar = _heardSoFar.isEmpty ? text : '$_heardSoFar $text';
    _onResult?.call(Heard(_heardSoFar, settled: settled));
  }

  /// endSegment : The recogniser stopping by itself, as Chrome's does
  /// after a short silence even mid-sentence. Not the user finishing.
  void endSegment() => _onDone?.call();

  @override
  Future<void> stop() async {
    listening = false;
    _onDone?.call();
    if (messyStop) {
      final late = lateResult;
      if (late != null) _onResult?.call(Heard(late, settled: true));
      _onDone?.call();
    }
  }

  @override
  Future<void> cancel() async {
    listening = false;
    _heardSoFar = '';
  }

  @override
  Future<void> dispose() async {}
}

Future<void> settle([int rounds = 10]) async {
  for (var i = 0; i < rounds; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  group('the speech queue', () {
    // Messages arrive faster than they can be spoken: FRIDAY says "Let me
    // look into that" and the answer lands before that sentence is done.
    // Without a queue the second cuts off the first, which is the thing
    // that makes a voice assistant sound broken.
    test('speaks one thing at a time, in order', () async {
      final voice = RecordingVoice(instant: false);
      final speaker = Speaker(voice);

      speaker.say('first');
      speaker.say('second');
      speaker.say('third');
      await settle();

      expect(voice.said, ['first'], reason: 'it started more than one');
      expect(speaker.pending, 2);

      voice.finish();
      await settle();
      expect(voice.said, ['first', 'second']);

      voice.finish();
      await settle();
      expect(voice.said, ['first', 'second', 'third']);

      // The third has started but not finished, so it is still speaking.
      expect(speaker.speaking, isTrue);

      voice.finish();
      await settle();
      expect(speaker.speaking, isFalse);
      expect(speaker.pending, 0);
    });

    test('drops empty text rather than pausing for it', () async {
      final voice = RecordingVoice();
      final speaker = Speaker(voice);

      speaker.say('');
      speaker.say('   ');
      await settle();

      expect(voice.said, isEmpty);
    });

    // Continuing to read an answer the user has already talked over is the
    // worst thing a voice assistant does.
    test('silencing stops now and forgets the rest', () async {
      final voice = RecordingVoice(instant: false);
      final speaker = Speaker(voice);

      speaker.say('first');
      speaker.say('second');
      await settle();

      await speaker.silence();

      expect(voice.silences, greaterThan(0));
      expect(speaker.pending, 0);
      await settle();
      expect(voice.said, ['first'], reason: 'it went on to the next');
    });

    // A failure replaces the progress that preceded it: reading out "let
    // me look into that" after "I could not reach GitLab" is nonsense.
    test('an urgent utterance clears what was queued', () async {
      final voice = RecordingVoice(instant: false);
      final speaker = Speaker(voice);

      speaker.say('looking');
      speaker.say('still looking');
      await settle();
      expect(speaker.pending, 1);

      speaker.say('I could not reach GitLab.', interrupts: true);
      await settle();

      expect(speaker.pending, 0);
      expect(voice.said.last, 'I could not reach GitLab.');
      expect(voice.said, isNot(contains('still looking')));
    });

    test(
      'says nothing while muted, and stops when muted mid-sentence',
      () async {
        final voice = RecordingVoice(instant: false);
        final speaker = Speaker(voice, enabled: false);

        speaker.say('ignored');
        await settle();
        expect(voice.said, isEmpty);

        await speaker.setEnabled(true);
        speaker.say('heard');
        await settle();
        expect(voice.said, ['heard']);

        await speaker.setEnabled(false);
        expect(voice.silences, greaterThan(0));
      },
    );

    // Speech failing without a word said about it is the worst of both:
    // nothing is heard, and there is nothing to act on. A browser
    // refusing to play audio on a page nobody has touched is the common
    // case, and asking by voice involves no click at all.
    test('a refusal to speak is reported, not swallowed', () async {
      final voice = _RefusingVoice(
        'Your browser will not let me speak until you click the page once.',
      );
      final speaker = Speaker(voice);

      speaker.say('an answer nobody will hear');
      await settle();

      expect(
        speaker.problem,
        contains('click the page'),
        reason: 'it went silent without saying why',
      );
    });

    test('the report clears once it can speak again', () async {
      final voice = _RefusingVoice('blocked');
      final speaker = Speaker(voice);

      speaker.say('first');
      await settle();
      expect(speaker.problem, isNotNull);

      voice.refusing = false;
      speaker.say('second');
      await settle();
      expect(speaker.problem, isNull);
    });

    // A synthesiser that throws should not stop the conversation; the user
    // can still read it.
    test('carries on when the synthesiser fails', () async {
      final voice = _BrokenVoice();
      final speaker = Speaker(voice);

      speaker.say('one');
      speaker.say('two');
      await settle();

      expect(voice.attempts, 2);
      expect(speaker.speaking, isFalse);
    });
  });

  group('speaking a conversation', () {
    /// sessionOn : A conversation with a scripted stream, bound to voice.
    Future<
      (VoiceSession, Conversation, RecordingVoice, ScriptedEars, FakeServer)
    >
    sessionOn(List<StreamAnswer> answers) async {
      final server = FakeServer();
      final api = loggedIn(server, bytes: ScriptedByteSource(answers));
      await authorise(api);

      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final voice = RecordingVoice();
      final ears = ScriptedEars();
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(voice),
        ears: ears,
      );
      return (session, conversation, voice, ears, server);
    }

    test('reads each message as it arrives, then the answer', () async {
      final (session, conversation, voice, _, server) = await sessionOn([
        StreamAnswer(
          chunks: [
            sse('update', seq: 1, text: 'Let me look into that.'),
            sse('final', seq: 2, text: 'Go is a language.'),
          ],
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await conversation.send('what is Go?');
      await settle(20);

      expect(voice.said, ['Let me look into that.', 'Go is a language.']);
      session.dispose();
    });

    test('stops talking when the user talks over it', () async {
      final (session, conversation, voice, _, server) = await sessionOn([
        StreamAnswer(
          chunks: [sse('update', seq: 1, text: 'working')],
          hold: true,
        ),
        StreamAnswer(chunks: [sse('final', seq: 1, text: 'second answer')]),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);
      server.on('GET /v1/chats/chat_01DDD', chatJson(status: 'cancelled'));

      await conversation.send('first');
      await settle(8);
      final before = voice.silences;

      await conversation.send('no, this instead');
      await settle(8);

      expect(
        voice.silences,
        greaterThan(before),
        reason: 'it kept reading the answer that was talked over',
      );
      session.dispose();
    });

    test('a failure is read at once, not after the progress', () async {
      final (session, conversation, voice, _, server) = await sessionOn([
        StreamAnswer(
          chunks: [
            sse('update', seq: 1, text: 'trying'),
            sse('error', text: 'I could not reach GitLab.'),
          ],
        ),
      ]);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await conversation.send('check GitLab');
      await settle(20);

      expect(voice.said.last, 'I could not reach GitLab.');
      session.dispose();
    });
  });

  group('listening', () {
    Future<
      (VoiceSession, Conversation, RecordingVoice, ScriptedEars, FakeServer)
    >
    sessionWith(
      ScriptedEars ears, {
      Chime? chime,
      StayingAwake? stayingAwake,
    }) async {
      final server = FakeServer();
      final api = loggedIn(server, bytes: ScriptedByteSource([StreamAnswer()]));
      await authorise(api);
      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final voice = RecordingVoice();
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(voice),
        ears: ears,
        // Silent unless a test is watching for it: the real one reaches for
        // the platform, which a test has no business doing.
        chime: chime ?? () async {},
        stayingAwake: stayingAwake ?? const NothingToHold(),
      );
      return (session, conversation, voice, ears, server);
    }

    test('sends what was heard once the speaker stops', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what is');
      ears.hear('what is a goroutine?', settled: true);
      await settle();

      expect(session.heard, 'what is a goroutine?');

      await session.stopListening();
      await settle(10);

      expect(conversation.exchanges.single.prompt, 'what is a goroutine?');
      expect(session.listening, isFalse);
      session.dispose();
    });

    // A microphone opened by accident hears nothing. Sending that would
    // put an empty prompt to the model and get a confused answer back.
    // One question spoken must become one prompt, however untidily the
    // recogniser reports that it has stopped.
    test('sends once even when the recogniser stops messily', () async {
      final ears = ScriptedEars(messyStop: true)
        ..lateResult = 'what do you know about time machine';
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what do you know about time machine', settled: true);
      await settle();

      await session.stopListening();
      await settle(10);

      expect(
        conversation.exchanges.length,
        1,
        reason:
            'one utterance became ${conversation.exchanges.length} '
            'prompts, so the second cancelled the first',
      );
      expect(
        conversation.exchanges.single.prompt,
        'what do you know about time machine',
      );
      session.dispose();
    });

    // Tapping the microphone to stop, when the recogniser has already
    // stopped itself, must not send a second time either.
    test('stopping after it already stopped sends nothing more', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('how are you', settled: true);
      await settle();
      await session.stopListening();
      await settle(10);

      // Tapping again, after it has already stopped, must add nothing.
      await session.stopListening();
      await settle(10);

      expect(conversation.exchanges.length, 1);
      session.dispose();
    });

    // Chrome's recogniser stops on its own after a short silence, which
    // happens mid-sentence while someone is thinking. Treating that as the
    // end of the question sends half of it.
    test('a pause mid-sentence does not end the turn', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what do you know about', settled: true);
      await settle();

      // The engine gives up; the user has not.
      ears.endSegment();
      await settle();

      expect(
        conversation.exchanges,
        isEmpty,
        reason: 'half a question was sent when the engine paused',
      );
      expect(session.listening, isTrue, reason: 'it stopped listening');

      // The rest of the sentence, in a fresh segment.
      ears.hear('time machines', settled: true);
      await settle();
      expect(session.heard, 'what do you know about time machines');

      await session.stopListening();
      await settle(10);

      expect(
        conversation.exchanges.single.prompt,
        'what do you know about time machines',
      );
      session.dispose();
    });

    // Tapping the microphone ends the turn even though the engine stopping
    // would not.
    test('the user stopping does end the turn', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('how are you', settled: true);
      await settle();

      await session.stopListening();
      await settle(10);

      expect(session.listening, isFalse);
      expect(conversation.exchanges.single.prompt, 'how are you');
      session.dispose();
    });

    // The wait after speaking must be the silence budget alone. Waiting
    // for the recogniser to report that it had stopped, and only then
    // counting, stacked its timeout on top and left a long dead pause.
    test('the silence timer ends the turn, not the recogniser', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what is a mutex', settled: true);
      await settle();

      // The engine never reports anything further — no onDone at all.
      expect(conversation.exchanges, isEmpty);

      // Only the passing of the silence budget should finish it.
      await Future<void>.delayed(const Duration(milliseconds: 2400));
      await settle(10);

      expect(
        conversation.exchanges.single.prompt,
        'what is a mutex',
        reason: 'the turn never ended without the engine saying so',
      );
      expect(session.listening, isFalse);
      session.dispose();
    });

    // Every new word pushes the end of the turn back, so speaking
    // continuously is never cut off.
    test('new words restart the countdown', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      for (final words in ['what', 'what is', 'what is a mutex']) {
        ears.hear(words, settled: false);
        await Future<void>.delayed(const Duration(milliseconds: 900));
      }

      // Well past the budget in total, but never 2s without a new word.
      expect(
        conversation.exchanges,
        isEmpty,
        reason: 'it gave up while the user was still talking',
      );
      expect(session.listening, isTrue);

      await session.stopListening();
      await settle(10);
      expect(conversation.exchanges.single.prompt, 'what is a mutex');
      session.dispose();
    });

    test('sends nothing when it heard nothing', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, _) = await sessionWith(ears);

      await session.startListening();
      await session.stopListening();
      await settle();

      expect(conversation.exchanges, isEmpty);
      session.dispose();
    });

    // FRIDAY must not hear itself and answer its own voice.
    test('silences the answer before opening the microphone', () async {
      final ears = ScriptedEars();
      final (session, _, voice, _, _) = await sessionWith(ears);

      await session.startListening();

      expect(voice.silences, greaterThan(0));
      session.dispose();
    });

    // FRIDAY talking while the microphone is open means it hears itself
    // and answers its own voice.
    test('says nothing aloud while the microphone is open', () async {
      final (answer, chunks) = StreamAnswer.manual();
      final server = FakeServer();
      final api = loggedIn(server, bytes: ScriptedByteSource([answer]));
      await authorise(api);
      server.on('POST /v1/chats', chatJson(), status: 202);

      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final voice = RecordingVoice();
      final ears = ScriptedEars();
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(voice),
        ears: ears,
      );

      await conversation.send('a question');
      await settle(6);

      // The microphone opens while the chat is still running.
      await session.startListening();
      await settle(4);

      // An answer arrives mid-listen. It must not be read out.
      chunks.add(utf8.encode(sse('final', seq: 1, text: 'the answer')));
      await settle(10);

      expect(
        conversation.exchanges.single.answer,
        'the answer',
        reason: 'the answer should still be received and shown',
      );
      expect(
        voice.said,
        isEmpty,
        reason: 'it read the answer aloud into an open microphone',
      );

      await chunks.close();
      session.dispose();
    });

    test('cancelling throws away what was heard', () async {
      final ears = ScriptedEars();
      final (session, conversation, _, _, _) = await sessionWith(ears);

      await session.startListening();
      ears.hear('never mind', settled: true);
      await settle();

      await session.cancelListening();
      await settle();

      expect(session.heard, isEmpty);
      expect(conversation.exchanges, isEmpty);
      session.dispose();
    });

    // The chime is what tells the speaker to carry on, so it has to land
    // while they are still talking, not when the turn is over.
    test('chimes as soon as the name is heard, once', () async {
      var chimes = 0;
      final ears = ScriptedEars();
      final (session, _, _, _, _) = await sessionWith(
        ears,
        chime: () async => chimes++,
      );
      await session.setAwake(true);

      ears.hear('friday');
      await settle();
      expect(chimes, 1, reason: 'the name was heard and not acknowledged');

      // Several more partials of the same turn must not each ping.
      ears.hear('friday what is');
      await settle();
      ears.hear('friday what is a mutex');
      await settle();
      expect(chimes, 1, reason: 'it pinged again within the same turn');

      session.dispose();
    });

    test('does not chime at speech that was not addressed to it', () async {
      var chimes = 0;
      final ears = ScriptedEars();
      final (session, _, _, _, _) = await sessionWith(
        ears,
        chime: () async => chimes++,
      );
      await session.setAwake(true);

      ears.hear('what is a mutex');
      await settle();

      expect(chimes, 0);
      session.dispose();
    });

    // Pressing the button is itself an acknowledgement; a ping on top is
    // noise.
    test('does not chime when the microphone was opened by hand', () async {
      var chimes = 0;
      final ears = ScriptedEars();
      final (session, _, _, _, _) = await sessionWith(
        ears,
        chime: () async => chimes++,
      );

      await session.startListening();
      ears.hear('friday what is a mutex');
      await settle();

      expect(chimes, 0);
      session.dispose();
    });

    // The foreground service is what lets a backgrounded app hold the
    // microphone, and Android refuses it without permission to show its
    // notification. Worth saying — but not a reason to stop: on a charger
    // with the app on screen, FRIDAY hears everything without it.
    test(
      'a refused foreground service is reported but still listens',
      () async {
        final ears = ScriptedEars();
        final (session, _, _, _, _) = await sessionWith(
          ears,
          stayingAwake: const _Refuses(
            'I need permission to show a notification.',
          ),
        );

        await session.setAwake(true);

        expect(session.awake, isTrue, reason: 'it gave up over a notification');
        expect(session.listening, isTrue);
        expect(session.problem, contains('notification'));
        session.dispose();
      },
    );

    test('turning always awake off releases what was held', () async {
      final held = _Counts();
      final ears = ScriptedEars();
      final (session, _, _, _, _) = await sessionWith(ears, stayingAwake: held);

      await session.setAwake(true);
      expect(held.begun, 1);
      await session.setAwake(false);
      expect(held.ended, 1);

      session.dispose();
    });

    // The whole point of always awake: a phone left listening on a charger
    // must still be listening an hour later. The owner's words — "the mic
    // should never stop no mattter what when always awakw option is
    // chosen". Before this, six failures in a row switched it off and the
    // only way to find out was to ask something and get nothing back.
    test('always awake keeps trying however often opening fails', () async {
      final ears = ScriptedEars(canHear: false)
        ..refusal = 'the recogniser is busy';
      final (session, _, _, _, _) = await sessionWith(ears);

      await session.setAwake(true);

      // Far past the six that used to end it.
      for (var attempt = 0; attempt < 12; attempt++) {
        await Future<void>.delayed(const Duration(milliseconds: 40));
        await settle();
      }

      expect(session.awake, isTrue, reason: 'it gave up');
      expect(session.state, isNot(ListeningState.unavailable));
      expect(
        session.problem,
        isNotNull,
        reason: 'it should say so while it is failing',
      );

      // And it recovers by itself, without anybody touching the phone.
      ears.canHear = true;
      await Future<void>.delayed(const Duration(milliseconds: 400));
      await settle(6);
      expect(session.awake, isTrue);

      session.dispose();
    });

    // An engine that measured the silence is not second-guessed. Whisper
    // waits three seconds of its own before calling anything settled, and
    // waiting again on top of that was the bug that made answering feel
    // slow — the same pause counted twice.
    test('an engine that endpoints itself is not waited on again', () async {
      final ears = ScriptedEars()..endpointsItself = true;
      final (session, conversation, _, _, server) = await sessionWith(ears);
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what is a goroutine?', settled: true);
      await settle(6);

      expect(
        conversation.exchanges.single.prompt,
        'what is a goroutine?',
        reason: 'it waited again after the engine had already waited',
      );
      session.dispose();
    });

    test('reports a microphone it cannot use', () async {
      final ears = ScriptedEars(canHear: false);
      final (session, _, _, _, _) = await sessionWith(ears);

      await session.startListening();

      expect(session.state, ListeningState.unavailable);
      expect(session.problem, isNotNull);
      expect(session.listening, isFalse);
      session.dispose();
    });

    // The reason travels, rather than being replaced by a guess. Whisper
    // failing to download its weights was reported as a microphone
    // permission problem, which sent the user to a settings screen where
    // there was nothing wrong and nothing to fix.
    test(
      'reports what actually stopped it, not always the microphone',
      () async {
        final ears = ScriptedEars(canHear: false)
          ..refusal =
              'I could not download the speech model. Check the network.';
        final (session, _, _, _, _) = await sessionWith(ears);

        await session.startListening();

        expect(session.problem, contains('speech model'));
        expect(session.problem, isNot(contains('permission')));
        session.dispose();
      },
    );
  });
  // The browser refuses to start recognition while the previous one is
  // still running, and it stays running for a moment after the plugin
  // believes it has stopped. Always-awake reopens after every utterance,
  // so that race is hit constantly.
  test('an engine that is still running does not break listening', () async {
    final ears = ScriptedEars(refuseOverlap: true);
    final server = FakeServer();
    final api = loggedIn(server, bytes: ScriptedByteSource([StreamAnswer()]));
    await authorise(api);
    final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
    final session = VoiceSession(
      conversation: conversation,
      speaker: Speaker(RecordingVoice()),
      ears: ears,
    );
    server.on('POST /v1/chats', chatJson(), status: 202);

    await session.setAwake(true);
    await settle();

    // A question, then the microphone reopening on top of an engine that
    // has not finished letting go.
    ears.hear('friday what is a mutex', settled: true);
    await settle();
    ears.endSegment();
    await Future<void>.delayed(const Duration(milliseconds: 2600));
    await settle(10);

    expect(conversation.exchanges.single.prompt, 'friday what is a mutex');
    expect(
      session.problem,
      isNull,
      reason: 'the overlap was reported to the user as a failure',
    );
    expect(
      session.listening,
      isTrue,
      reason: 'it stopped listening after the overlap',
    );
    session.dispose();
  });

  group('always awake', () {
    Future<(VoiceSession, Conversation, ScriptedEars, FakeServer)>
    awakeSession() async {
      final server = FakeServer();
      final api = loggedIn(server, bytes: ScriptedByteSource([StreamAnswer()]));
      await authorise(api);
      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final ears = ScriptedEars();
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(RecordingVoice()),
        ears: ears,
      );
      return (session, conversation, ears, server);
    }

    /// says : One complete utterance, ended by the user pausing.
    Future<void> says(
      VoiceSession session,
      ScriptedEars ears,
      String words,
    ) async {
      ears.hear(words, settled: true);
      await settle();
      ears.endSegment();
      await Future<void>.delayed(const Duration(milliseconds: 2300));
      await settle(10);
    }

    // An always-open microphone is something the user turns on, not
    // something they discover.
    test('starts off, and opens the microphone when turned on', () async {
      final (session, _, _, _) = await awakeSession();

      expect(session.awake, isFalse);
      expect(session.listening, isFalse);

      await session.setAwake(true);
      expect(session.listening, isTrue);
      session.dispose();
    });

    // The whole point: the name is what makes it a question for FRIDAY.
    test('answers what begins with the name', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'friday what is a mutex');

      expect(
        conversation.exchanges.single.prompt,
        'friday what is a mutex',
        reason: 'the name is left in; nothing is taken off',
      );
      session.dispose();
    });

    // Surrounding speech is not addressed to FRIDAY. Without this gate
    // every sentence in the room is a prompt, and a bill.
    //
    // Speech that happens to contain the name is answered — the accepted
    // cost of matching it wherever it is said rather than only first.
    test('ignores the room talking', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      for (final overheard in [
        'did you watch the match last night',
        'what is a mutex',
        'tell me when you are ready',
      ]) {
        await says(session, ears, overheard);
      }

      expect(
        conversation.exchanges,
        isEmpty,
        reason: 'the room reached the model',
      );
      expect(session.listening, isTrue, reason: 'it stopped listening');
      session.dispose();
    });

    // The name alone is sent like anything else rather than held as a
    // summons: nothing is taken off, so there is no empty question to
    // wait on, and FRIDAY answering "yes?" is a perfectly good reply.
    test('the name alone is sent', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'friday');

      expect(conversation.exchanges.single.prompt, 'friday');
      session.dispose();
    });

    // Having answered, it must want the name again — otherwise the next
    // thing anyone says becomes a prompt.
    test('the name is needed again for the next question', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'friday what is a mutex');
      await says(session, ears, 'that is interesting');

      expect(
        conversation.exchanges.length,
        1,
        reason: 'it answered something not addressed to it',
      );
      session.dispose();
    });

    test('several questions in a row, hands free', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'friday what is a mutex');
      await says(session, ears, 'hey friday what is a channel');

      expect(conversation.exchanges.map((e) => e.prompt), [
        'friday what is a mutex',
        'hey friday what is a channel',
      ]);
      session.dispose();
    });

    // The microphone is open all the time when always awake. Refusing to
    // speak over it, which is right for the button, means never speaking
    // at all here — FRIDAY goes completely silent.
    test('still speaks even though the microphone is open', () async {
      final (answer, chunks) = StreamAnswer.manual();
      final server = FakeServer();
      final api = loggedIn(server, bytes: ScriptedByteSource([answer]));
      await authorise(api);
      server.on('POST /v1/chats', chatJson(), status: 202);

      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final voice = RecordingVoice();
      final ears = ScriptedEars();
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(voice),
        ears: ears,
      );

      await session.setAwake(true);
      await says(session, ears, 'friday what is a mutex');
      expect(conversation.exchanges.single.prompt, 'friday what is a mutex');

      chunks.add(utf8.encode(sse('final', seq: 1, text: 'A mutex is a lock.')));
      await settle(12);

      expect(
        voice.said,
        contains('A mutex is a lock.'),
        reason: 'it never said the answer out loud',
      );

      await chunks.close();
      session.dispose();
    });

    // "FRIDAY, stop" while it is talking must stop it.
    test('being spoken to silences the answer', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'friday what is a mutex');
      final before = conversation.exchanges.length;

      await says(session, ears, 'friday stop');

      expect(
        conversation.exchanges.length,
        before + 1,
        reason: 'the interruption was not taken as a question',
      );
      session.dispose();
    });

    // On a laptop the speaker feeds back into the microphone. An open
    // microphone transcribes FRIDAY's own answer: words appear in the box
    // that nobody said, and each one restarts the silence countdown so
    // the turn never ends.
    /// speaking : A session part-way through reading an answer aloud,
    /// with the microphone still open.
    Future<(VoiceSession, Conversation, ScriptedEars, RecordingVoice)>
    speakingSession(String answer, {bool accumulates = false}) async {
      final (stream, chunks) = StreamAnswer.manual();
      final server = FakeServer();
      final api = loggedIn(
        server,
        // A second stream for the follow-up, since a correction spoken
        // over an answer produces another chat.
        bytes: ScriptedByteSource([
          stream,
          StreamAnswer(chunks: [sse('final', seq: 1, text: 'and channels')]),
        ]),
      );
      await authorise(api);
      server.on('POST /v1/chats', chatJson(), status: 202);

      final conversation = Conversation(api: api)..sessionId = 'sess_01CCC';
      final voice = RecordingVoice(instant: false);
      final ears = ScriptedEars(accumulates: accumulates);
      final session = VoiceSession(
        conversation: conversation,
        speaker: Speaker(voice),
        ears: ears,
      );

      await session.setAwake(true);
      ears.hear('friday what is a mutex', settled: true);
      await settle();
      ears.endSegment();
      await Future<void>.delayed(const Duration(milliseconds: 2300));
      await settle(10);

      chunks.add(utf8.encode(sse('final', seq: 1, text: answer)));
      await settle(12);
      return (session, conversation, ears, voice);
    }

    // Closing the microphone while FRIDAY talks locks the user out for
    // the whole of a long answer, and cancelling or correcting by voice
    // is the thing a voice assistant most needs to allow.
    test('keeps the microphone open while talking', () async {
      final (session, _, ears, voice) = await speakingSession(
        'A mutex is a lock.',
      );

      expect(voice.said, contains('A mutex is a lock.'));
      expect(
        session.listening,
        isTrue,
        reason: 'the user cannot interrupt a long answer',
      );
      expect(ears.listening, isTrue);
      session.dispose();
    });

    // On a laptop the speaker feeds back into the microphone, so the
    // answer arrives as a transcript. It must not appear in the box.
    test('ignores its own voice coming back', () async {
      final (session, conversation, ears, _) = await speakingSession(
        'A mutex is a lock that protects state.',
      );
      final before = conversation.exchanges.length;

      ears.hear('a mutex is a lock that protects', settled: false);
      await settle();

      expect(
        session.heard,
        isEmpty,
        reason: "FRIDAY's own answer was typed into the input",
      );
      expect(
        conversation.exchanges.length,
        before,
        reason: 'it answered its own voice',
      );
      session.dispose();
    });

    // The name is what separates the user from the echo. It cannot be
    // required at the front: the echo gets there first.
    test(
      'being interrupted by name stops it and takes the correction',
      () async {
        final (session, conversation, ears, voice) = await speakingSession(
          'A mutex is a lock that protects state.',
        );
        final before = conversation.exchanges.length;
        final silencesBefore = voice.silences;

        // The echo, and then the user talking over it.
        ears.hear(
          'a mutex is a lock friday no tell me about channels',
          settled: true,
        );
        await settle();

        expect(
          voice.silences,
          greaterThan(silencesBefore),
          reason: 'it carried on talking over the user',
        );
        expect(
          session.heard,
          'friday no tell me about channels',
          reason: 'the echo before the name was kept',
        );

        ears.endSegment();
        await Future<void>.delayed(const Duration(milliseconds: 2300));
        await settle(10);

        expect(conversation.exchanges.length, before + 1);
        expect(
          conversation.exchanges.last.prompt,
          'friday no tell me about channels',
          reason: 'the echo before the name was dropped, the name kept',
        );
        session.dispose();
      },
    );

    // The echo is ignored as it arrives, but the recogniser keeps
    // accumulating it. After a long answer the transcript holds the
    // whole paragraph, and the next thing the user says lands on the end
    // of it — so what gets sent is the answer read back with a question
    // attached, long enough to be refused, which looks like being
    // ignored.
    test('forgets the echo once it has finished talking', () async {
      const paragraph =
          'The Himalayas are the highest mountain range, '
          'stretching across five countries and home to Mount Everest.';
      final (session, conversation, ears, voice) = await speakingSession(
        paragraph,
        accumulates: true,
      );
      final before = conversation.exchanges.length;

      // The answer coming back through the microphone while it is read.
      ears.hear(paragraph.toLowerCase(), settled: false);
      await settle();
      expect(session.heard, isEmpty, reason: 'the echo reached the input');

      // It finishes speaking.
      voice.finish();
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await settle(10);

      expect(
        session.heard,
        isEmpty,
        reason: 'the echo was left in the transcript',
      );
      expect(session.listening, isTrue, reason: 'it stopped listening');

      // Now a question, into what must be a clean transcript.
      await says(session, ears, 'friday what about K2');

      expect(
        conversation.exchanges.length,
        before + 1,
        reason: 'the question after a long answer was not taken',
      );
      expect(
        conversation.exchanges.last.prompt,
        'friday what about K2',
        reason: 'the echo was sent along with the question',
      );
      session.dispose();
    });

    // If the answer itself says the name, an interruption cannot be told
    // from the echo, so none is claimed.
    test('does not mistake its own mention of the name', () async {
      final (session, conversation, ears, _) = await speakingSession(
        'I will remind you on friday afternoon.',
      );
      final before = conversation.exchanges.length;

      ears.hear('i will remind you on friday afternoon', settled: true);
      await settle();

      expect(
        conversation.exchanges.length,
        before,
        reason: 'it interrupted itself',
      );
      session.dispose();
    });

    // Said and ignored: the greeting was thrown away as a lead-in,
    // leaving nothing, so it was taken for a summons and waited for a
    // question that never came.
    test('answers being greeted', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);
      await session.setAwake(true);

      await says(session, ears, 'hello friday');

      expect(
        conversation.exchanges.single.prompt,
        'hello friday',
        reason: 'being greeted was ignored',
      );
      session.dispose();
    });

    test('turning it off closes the microphone at once', () async {
      final (session, conversation, ears, _) = await awakeSession();
      await session.setAwake(true);
      ears.hear('friday never mind', settled: true);
      await settle();

      await session.setAwake(false);
      await settle();

      expect(session.listening, isFalse);
      expect(
        conversation.exchanges,
        isEmpty,
        reason: 'it sent what it had heard despite being turned off',
      );
      session.dispose();
    });

    // With the button, pressing it is itself how the user says who they
    // are talking to, so no name is needed.
    test('the button needs no name', () async {
      final (session, conversation, ears, server) = await awakeSession();
      server.on('POST /v1/chats', chatJson(), status: 202);

      await session.startListening();
      ears.hear('what is a mutex', settled: true);
      await settle();
      await session.stopListening();
      await settle(10);

      expect(conversation.exchanges.single.prompt, 'what is a mutex');
      session.dispose();
    });
  });
}

/// _BrokenVoice : A synthesiser that always fails.
class _BrokenVoice implements Voice {
  int attempts = 0;

  @override
  Future<bool> available() async => true;

  @override
  Future<void> say(String text) async {
    attempts++;
    throw StateError('the engine is broken');
  }

  @override
  Future<void> silence() async {}

  @override
  Future<void> dispose() async {}
}

/// _RefusingVoice : A synthesiser that declines to speak, as a browser
/// does on a page that has had no user gesture.
class _RefusingVoice implements Voice {
  _RefusingVoice(this.reason);

  final String reason;
  bool refusing = true;

  @override
  Future<bool> available() async => true;

  @override
  Future<void> say(String text) async {
    if (refusing) throw VoiceRefused(reason);
  }

  @override
  Future<void> silence() async {}

  @override
  Future<void> dispose() async {}
}

/// _Refuses : A platform that will not let the microphone be held.
class _Refuses implements StayingAwake {
  const _Refuses(this.why);

  final String why;

  @override
  Future<String?> begin() async => why;

  @override
  Future<void> end() async {}
}

/// _Counts : Records how often it was asked to hold and release.
class _Counts implements StayingAwake {
  int begun = 0;
  int ended = 0;

  @override
  Future<String?> begin() async {
    begun++;
    return null;
  }

  @override
  Future<void> end() async => ended++;
}
