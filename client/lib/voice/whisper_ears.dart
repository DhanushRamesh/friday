/// Listening with Whisper, on the phone itself.
///
/// Android's own recogniser is quick and free, but it is trained for
/// dictation in a handful of accents and gives up on anything it is unsure
/// of. Whisper hears Indian-accented English considerably better, which is
/// the whole reason for carrying half a gigabyte of weights around.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:record/record.dart';
import 'package:whisper_ggml/whisper_ggml.dart';

import 'endpointer.dart';
import 'listener.dart';
import 'transcript.dart';
import 'whisper_model_store.dart';

/// _chosenModel : Which weights to hear with, set at build time with
/// `--dart-define=FRIDAY_WHISPER_MODEL=small.en`.
const String _chosenModel = String.fromEnvironment(
  'FRIDAY_WHISPER_MODEL',
  defaultValue: 'base.en',
);

/// _model : The weights named by _chosenModel.
///
/// base.en by default, because latency is what decides whether voice is
/// usable. small.en hears an Indian accent better and was the first choice,
/// but on a real phone it decoded slower than the speaking, so the words
/// arrived long after they were said and sometimes not at all: whisper_ggml
/// keeps feeding a window that grows faster than it drains. base.en is a
/// quarter of the size and roughly three times quicker.
///
/// Worth re-testing on a faster phone — `make apk WHISPER_MODEL=small.en`.
final WhisperModel _model = WhisperModel.values.firstWhere(
  (m) => m.modelName == _chosenModel,
  orElse: () => WhisperModel.baseEn,
);

/// _sampleRate : What Whisper expects, and therefore what is recorded.
const int _sampleRate = 16000;

/// _pauseEndsAQuestion : How long a silence means the question is over.
///
/// The one rule of listening: say what you like, stop for three seconds,
/// and it is sent. Nothing else ends a question — not the engine, not a
/// timer counted somewhere else, not the microphone, which never stops.
const Duration _pauseEndsAQuestion = Duration(seconds: 3);

/// _maxSegment : The longest single stretch, whatever the endpointer
/// thinks. Whisper's window is thirty seconds of audio and latency grows
/// with the buffer, so a stretch ends well before that even if the speaker
/// has not paused. How soon a pause ends one is Endpointer's business.
const Duration _maxSegment = Duration(seconds: 20);

/// _voiceFloor : The quietest chunk that counts as speech, as RMS of
/// samples scaled to ±1. Below this is a quiet room, not a quiet talker.
const double _voiceFloor = 0.0015;

/// _voiceOverNoise : How far above the measured noise floor a chunk must
/// be to count as speech. The floor adapts, so a fan or a fridge raises the
/// bar rather than being heard as talking.
const double _voiceOverNoise = 2.5;

/// _noiseFloorCap : The loudest a room is allowed to be considered quiet.
/// Without a cap, a burst of speech early on would raise the floor so far
/// that nothing after it registers.
const double _noiseFloorCap = 0.01;

// No initial prompt. One was set — "A conversation with FRIDAY, a personal
// assistant. The speaker addresses her as Friday." — to bias the decoder
// towards the one word that must never be missed, since everything is gated
// on the name.
//
// whisper.cpp tokenises initial_prompt into the decoder's context, and on
// quiet or short audio the decoder simply carries on from it: the sentence
// came back as though it had been spoken, appeared in the input bar, and
// would have been sent to the model as a question. Any prompt can leak this
// way, so there is no safer wording — only no prompt.

/// WhisperEars : Hears by recording raw audio and decoding it on the
/// device.
class WhisperEars implements Ears {
  WhisperEars({this.language = 'en'});

  /// language : Which language to decode as. Whisper takes a bare language
  /// code, so a tag such as en-IN is cut down to its first part; the accent
  /// is not a separate language to it.
  final String language;

  final WhisperModelStore _store = WhisperModelStore(model: _model);
  final WhisperController _whisper = WhisperController();
  final AudioRecorder _recorder = AudioRecorder();

  /// _audio : The one microphone, shared by every stretch of listening.
  ///
  /// Opened once and kept open. Stopping and restarting the recorder
  /// between questions was a gap in which anything said was simply gone,
  /// and on Android it is also what made the platform recogniser tone —
  /// the reason this path exists at all is that nothing here restarts.
  StreamController<Uint8List>? _audio;
  StreamSubscription<Uint8List>? _mic;

  String? _modelPath;

  /// _segment : The stretch of listening currently running, if any.
  _Segment? _segment;

  /// _preparing : How far the one-time download has got.
  final ValueNotifier<double?> _preparing = ValueNotifier<double?>(null);

  @override
  ValueListenable<double?> get preparing => _preparing;

  /// Nothing to let go of: the recorder is stopped and started, and there is
  /// no recognition service holding anything between turns.
  /// Endpointer decides, having measured the silence, so a settled
  /// result is not a guess.
  @override
  bool get endpointsItself => true;

  @override
  Duration get settleBeforeReopen => const Duration(milliseconds: 150);

  @override
  Future<String?> prepare() async {
    if (!await _recorder.hasPermission()) {
      return 'I cannot use the microphone. Check the permission.';
    }

    if (_modelPath != null) return null;
    try {
      if (!await _store.ready()) {
        debugPrint(
          'FRIDAY: fetching the ${_model.modelName} speech model. '
          'This happens once and is about half a gigabyte.',
        );
      }
      var announced = -1;
      _modelPath = await _store.ensure(
        onProgress: (fraction) {
          _preparing.value = fraction ?? 0;
          // Logged in steps rather than continuously: this is the only
          // sign of life during a download that takes minutes, and the
          // first run looks broken without it.
          final percent = fraction == null ? -1 : (fraction * 100) ~/ 5 * 5;
          if (percent > announced) {
            announced = percent;
            debugPrint('FRIDAY: speech model $percent%');
          }
        },
      );
    } on Object catch (e) {
      debugPrint('FRIDAY: could not get the speech model: $e');
      // Named for what actually failed. Reported as a microphone problem,
      // this sent the user to a permission screen where there was nothing
      // wrong and nothing to fix.
      return 'I could not download the speech model. Check the network.';
    } finally {
      _preparing.value = null;
    }
    return null;
  }

  @override
  Future<void> listen({
    required void Function(Heard) onResult,
    required void Function() onDone,
    required void Function(String) onError,
  }) async {
    final problem = await prepare();
    if (problem != null) {
      onError(problem);
      return;
    }

    // Whatever the caller believes, only one stretch runs at a time: the
    // native side holds a single live session and a second one would be
    // refused.
    await cancel();

    try {
      await _openMicrophone();
      _segment = await _Segment.start(
        audio: _audio!.stream,
        whisper: _whisper,
        modelPath: _modelPath!,
        language: language.split('-').first,
        onResult: onResult,
        onDone: onDone,
      );
    } on Object catch (e) {
      debugPrint('FRIDAY: could not start listening: $e');
      onError('I could not open the microphone. Try again.');
    }
  }

  /// _openMicrophone : Starts recording, if it is not already.
  ///
  /// voiceRecognition is the source Android provides for a recogniser:
  /// minimal processing and no automatic gain, so the speech arrives as it
  /// was spoken rather than conditioned for a phone call. Echo cancellation
  /// is the one effect kept — without it FRIDAY's own answer comes back off
  /// the loudspeaker and lands inside the next question.
  ///
  /// manageBluetooth opens the SCO link, which is how earbuds are heard.
  Future<void> _openMicrophone() async {
    if (_mic != null) return;

    final stream = await _recorder.startStream(
      const RecordConfig(
        encoder: AudioEncoder.pcm16bits,
        sampleRate: _sampleRate,
        numChannels: 1,
        echoCancel: true,
        // Left off deliberately. All three together, over a
        // voiceCommunication source, flattened the signal until whisper
        // decoded silence; gain control does most of that, riding a quiet
        // room up and a voice down.
        noiseSuppress: false,
        autoGain: false,
        androidConfig: AndroidRecordConfig(
          audioSource: AndroidAudioSource.voiceRecognition,
          manageBluetooth: true,
        ),
      ),
    );

    // Broadcast, because each stretch subscribes in turn while the
    // microphone underneath them all keeps running.
    final audio = StreamController<Uint8List>.broadcast();
    _audio = audio;
    _mic = stream.listen(
      audio.add,
      onError: (Object e) => debugPrint('FRIDAY: microphone: $e'),
      onDone: audio.close,
    );
  }

  /// _closeMicrophone : Stops recording altogether.
  Future<void> _closeMicrophone() async {
    final mic = _mic;
    final audio = _audio;
    _mic = null;
    _audio = null;

    await mic?.cancel();
    await audio?.close();
    await _recorder.stop();
  }

  @override
  Future<void> stop() async {
    final segment = _segment;
    _segment = null;
    await segment?.finish(announce: true);
  }

  @override
  Future<void> cancel() async {
    final segment = _segment;
    _segment = null;
    await segment?.finish(announce: false);
  }

  @override
  Future<void> dispose() async {
    await cancel();
    await _closeMicrophone();
    _preparing.dispose();
    await _recorder.dispose();
    // The weights are parked in native memory between stretches so each one
    // does not pay the load again. Nothing is listening now, so give the
    // memory back.
    await _whisper.releaseModel();
  }
}

/// _Segment : One stretch of listening, from opening the microphone to the
/// text that came out of it.
class _Segment {
  _Segment._({
    required this.session,
    required this.toWhisper,
    required this.onResult,
    required this.onDone,
  });

  final WhisperLiveSession session;

  /// toWhisper : The audio on its way to the decoder. Closed when the
  /// stretch ends, because the recorder's own stream is cancelled rather
  /// than allowed to finish and nothing else would close it.
  final StreamController<Uint8List> toWhisper;
  final void Function(Heard) onResult;
  final void Function() onDone;

  StreamSubscription<Uint8List>? _audio;
  StreamSubscription<String>? _partials;
  Timer? _deadline;

  /// _heard : The most recent partial, kept so a stretch that is stopped
  /// rather than allowed to finish still reports what it had.
  String _heard = '';

  /// _finishing : Guards against finishing twice — silence and the caller
  /// stopping can arrive within a moment of each other.
  Future<void>? _finishing;

  /// _ears : Decides when the speaker has stopped.
  ///
  /// Three seconds, which is the owner's number and is the whole rule: talk
  /// for as long as you like, pause for three, and whatever was said goes
  /// if it had the name in it. Long enough to think mid-sentence, which is
  /// what every shorter value got wrong.
  final Endpointer _ears = Endpointer(
    sampleRate: _sampleRate,
    pauseAfterSpeech: _pauseEndsAQuestion,
  );

  static Future<_Segment> start({
    required Stream<Uint8List> audio,
    required WhisperController whisper,
    required String modelPath,
    required String language,
    required void Function(Heard) onResult,
    required void Function() onDone,
  }) async {
    // The microphone is already running and stays running. Only the
    // decoder is started and stopped here, which is what lets listening be
    // continuous: there is no moment when nothing is being recorded.
    final toWhisper = StreamController<Uint8List>();
    final WhisperLiveSession session;
    try {
      session = await whisper.transcribeLive(
        modelPath: modelPath,
        pcm16Stream: toWhisper.stream,
        lang: language,
        // Whisper marks silence and noise with tokens of its own —
        // [BLANK_AUDIO], (wind blowing), [MUSIC]. They are annotations, not
        // words, and one reached the user as though it were the answer.
        suppressNonSpeechTokens: true,
        // Parked between stretches: loading the weights takes seconds, and
        // a new stretch opens after every question.
        keepModelLoaded: true,
        gateRmsMin: _voiceFloor,
        gateVoiceRatio: _voiceOverNoise,
        gateNoiseFloorCap: _noiseFloorCap,
      );
    } on Object {
      await toWhisper.close();
      rethrow;
    }

    final segment = _Segment._(
      session: session,
      toWhisper: toWhisper,
      onResult: onResult,
      onDone: onDone,
    );

    segment._partials = session.partials.listen((text) {
      final said = spokenWords(text);
      segment._heard = said;
      if (said.isNotEmpty) onResult(Heard(said, settled: false));
    }, onError: (Object e) => debugPrint('FRIDAY: whisper: $e'));

    segment._audio = audio.listen((chunk) {
      if (!toWhisper.isClosed) toWhisper.add(chunk);
      segment._measure(chunk);
    }, onError: (Object e) => debugPrint('FRIDAY: microphone: $e'));

    segment._deadline = Timer(
      _maxSegment,
      () => segment.finish(announce: true),
    );
    return segment;
  }

  /// _measure : Ends the stretch once the speaker has stopped, or once it
  /// is clear nobody is going to start.
  void _measure(Uint8List chunk) {
    if (_ears.hear(chunk) != Ending.none) finish(announce: true);
  }

  /// finish : Stops recording, waits for the last of the audio to be
  /// decoded and reports the result.
  ///
  /// announce says whether the caller is to be told. A stretch that ended
  /// by itself is announced even when nothing was said, because that is
  /// what lets always-awake open the microphone again; a stretch the caller
  /// cancelled is silent, since the caller already knows. Either way the
  /// engine is shut down, or the next stretch cannot start.
  Future<void> finish({required bool announce}) {
    return _finishing ??= _finish(announce: announce);
  }

  Future<void> _finish({required bool announce}) async {
    _deadline?.cancel();
    // The subscription to the shared microphone ends; the microphone
    // itself does not. Nothing is deaf between one stretch and the next.
    await _audio?.cancel();
    await toWhisper.close();

    final began = DateTime.now();
    String text;
    try {
      text = await session.stop();
    } on Object catch (e) {
      debugPrint('FRIDAY: whisper would not finish: $e');
      text = _heard;
    }
    await _partials?.cancel();

    // Logged because it is the number the whole choice of model turns on,
    // and estimating it twice already led somewhere wrong. `adb logcat -s
    // flutter` says how long this phone actually takes.
    final took = DateTime.now().difference(began);
    debugPrint(
      'FRIDAY: whisper decoded the last of it in ${took.inMilliseconds}ms '
      '(${text.trim().length} chars)',
    );

    if (!announce) return;
    // Nothing said means nothing to send, but the caller is still told the
    // stretch is over so it can start another.
    final said = spokenWords(text);
    if (_ears.spoke && said.isNotEmpty) {
      onResult(Heard(said, settled: true));
    }
    onDone();
  }
}
