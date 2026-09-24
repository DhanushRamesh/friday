/// Listening with a streaming recogniser, on the phone itself.
///
/// Unlike Whisper, which is handed a stretch of audio and decodes it once
/// the speaking has stopped, a transducer emits words while they are being
/// said. There is no decode to wait for at the end and no session to
/// restart, so the microphone genuinely never stops and nothing makes a
/// noise when a question begins.
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:sherpa_onnx/sherpa_onnx.dart' as sherpa;

import 'downloading.dart';
import 'listener.dart';

/// _sampleRate : What the model expects, and therefore what is recorded.
const int _sampleRate = 16000;

/// _pauseEndsAQuestion : How long a silence means the question is over,
/// counted only once something has been said.
///
/// The owner's rule: talk as long as you like, stop for three seconds, and
/// it is sent. The recogniser counts it itself rather than being timed from
/// outside, which is the whole reason for using one that streams.
///
/// This is sherpa's *second* rule. Its first counts silence when nothing has
/// been decoded at all, and the two were the wrong way round here at first —
/// which meant an empty room reached an endpoint every three seconds and the
/// stream was reset over and over, quite possibly throwing away the opening
/// words of anything said into it.
const double _pauseEndsAQuestion = 3.0;

/// _patienceWithAnEmptyRoom : How long a silence with nothing said at all
/// counts as an ending.
///
/// Long, because it is not an ending. Always awake, the room is quiet
/// almost all the time and none of that is a question being finished.
const double _patienceWithAnEmptyRoom = 300.0;

/// _longestQuestion : Seconds after which a question ends regardless.
const double _longestQuestion = 30;

/// _threads : How many cores decoding may use.
///
/// Two. One is not quite real time on a mid-range phone and four leaves
/// nothing for the interface, which has to keep drawing the words as they
/// arrive.
const int _threads = 2;

/// _repository : Where the model files come from.
const String _repository =
    'https://huggingface.co/csukuangfj/'
    'sherpa-onnx-streaming-zipformer-en-2023-06-26/resolve/main';

/// _files : What has to be downloaded, and what it is called on disk.
///
/// The int8 encoder and joiner, because they are a third of the size for
/// no accuracy anybody has been able to hear. The decoder is left alone: it
/// is two megabytes either way and quantising it is where the quality
/// actually goes.
const Map<String, String> _files = {
  'encoder.onnx': 'encoder-epoch-99-avg-1-chunk-16-left-128.int8.onnx',
  'decoder.onnx': 'decoder-epoch-99-avg-1-chunk-16-left-128.onnx',
  'joiner.onnx': 'joiner-epoch-99-avg-1-chunk-16-left-128.int8.onnx',
  'tokens.txt': 'tokens.txt',
};

/// SherpaEars : Hears by streaming recorded audio through a transducer.
class SherpaEars implements Ears {
  SherpaEars({this.language = 'en'});

  /// language : Kept for the interface. This model is English only; a
  /// different language means a different model, not a different setting.
  final String language;

  final AudioRecorder _recorder = AudioRecorder();

  final ValueNotifier<double?> _preparing = ValueNotifier<double?>(null);

  sherpa.OnlineRecognizer? _recogniser;
  sherpa.OnlineStream? _stream;

  StreamSubscription<Uint8List>? _mic;

  void Function(Heard)? _onResult;
  void Function()? _onDone;

  /// _heard : How many chunks have arrived, and the loudest sample seen.
  /// Logged occasionally, because "no words at all" has four possible
  /// causes and they are told apart by whether audio is arriving, whether
  /// it has anything in it, and whether the decoder produces anything from
  /// it.
  int _chunks = 0;
  double _loudest = 0;

  /// _said : The last text reported, so an unchanged result is not
  /// announced again — a transducer reports on every chunk, several times a
  /// second, and most of them say the same thing.
  String _said = '';

  @override
  ValueListenable<double?> get preparing => _preparing;

  /// Nothing is restarted between questions: the recogniser is reset where
  /// it stands and the microphone never closes.
  @override
  Duration get settleBeforeReopen => Duration.zero;

  /// The recogniser decides, having counted the silence itself.
  @override
  bool get endpointsItself => true;

  @override
  Future<String?> prepare() async {
    if (!await _recorder.hasPermission()) {
      return 'I cannot use the microphone. Check the permission.';
    }
    if (_recogniser != null) return null;

    final String directory;
    try {
      directory = (await getApplicationSupportDirectory()).path;
      await _fetchModel(directory);
      for (final name in _files.keys) {
        final file = File('$directory/$name');
        final size = await file.exists() ? await file.length() : -1;
        debugPrint('FRIDAY: model $name is $size bytes');
      }
    } on Object catch (e) {
      debugPrint('FRIDAY: could not get the speech model: $e');
      return 'I could not download the speech model. Check the network.';
    } finally {
      _preparing.value = null;
    }

    try {
      sherpa.initBindings();
      _recogniser = sherpa.OnlineRecognizer(
        sherpa.OnlineRecognizerConfig(
          model: sherpa.OnlineModelConfig(
            transducer: sherpa.OnlineTransducerModelConfig(
              encoder: '$directory/encoder.onnx',
              decoder: '$directory/decoder.onnx',
              joiner: '$directory/joiner.onnx',
            ),
            tokens: '$directory/tokens.txt',
            numThreads: _threads,
          ),
          // The silence that ends a question is counted here, by the thing
          // that knows whether it is in the middle of a word.
          enableEndpoint: true,
          // Rule one is silence with nothing decoded; rule two is silence
          // after something was said. Only the second ends a question.
          rule1MinTrailingSilence: _patienceWithAnEmptyRoom,
          rule2MinTrailingSilence: _pauseEndsAQuestion,
          rule3MinUtteranceLength: _longestQuestion,
        ),
      );
      debugPrint('FRIDAY: recogniser ready');
    } on Object catch (e, where) {
      debugPrint('FRIDAY: could not start the recogniser: $e\n$where');
      return 'I could not start listening. Try again.';
    }
    return null;
  }

  /// _fetchModel : Downloads whatever is missing, reporting progress across
  /// the whole set rather than per file.
  Future<void> _fetchModel(String directory) async {
    var done = 0;
    for (final entry in _files.entries) {
      final at = done;
      await fetchTo(
        Uri.parse('$_repository/${entry.value}'),
        '$directory/${entry.key}',
        onProgress: (fraction) {
          // Weighted by file rather than by byte. The encoder is almost all
          // of it, so this is close enough and needs no sizes up front.
          final each = 1 / _files.length;
          _preparing.value = (at * each) + ((fraction ?? 0) * each);
        },
      );
      done++;
      _preparing.value = done / _files.length;
    }
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

    _onResult = onResult;
    _onDone = onDone;
    _said = '';

    // Already running: a question ended and the next is beginning. Nothing
    // is restarted — the stream is reset where it stands.
    if (_mic != null) {
      _resetStream();
      return;
    }

    try {
      _stream = _recogniser!.createStream();
      await _openMicrophone();
    } on Object catch (e) {
      debugPrint('FRIDAY: could not start listening: $e');
      onError('I could not open the microphone. Try again.');
    }
  }

  /// _openMicrophone : Starts recording, and keeps recording.
  ///
  /// voiceRecognition is the source Android provides for a recogniser:
  /// minimal processing, no automatic gain. Echo cancellation is kept,
  /// without which FRIDAY's own answer comes back off the loudspeaker and
  /// lands inside the next question.
  Future<void> _openMicrophone() async {
    final audio = await _recorder.startStream(
      const RecordConfig(
        encoder: AudioEncoder.pcm16bits,
        sampleRate: _sampleRate,
        numChannels: 1,
        echoCancel: true,
        noiseSuppress: false,
        autoGain: false,
        androidConfig: AndroidRecordConfig(
          audioSource: AndroidAudioSource.voiceRecognition,
          manageBluetooth: true,
        ),
      ),
    );

    _mic = audio.listen(
      _hear,
      onError: (Object e) => debugPrint('FRIDAY: microphone: $e'),
    );
  }

  /// _hear : Feeds one chunk through the recogniser and reports what
  /// changed.
  void _hear(Uint8List chunk) {
    final recogniser = _recogniser;
    final stream = _stream;
    if (recogniser == null || stream == null) return;

    final samples = chunk.lengthInBytes ~/ 2;
    if (samples == 0) return;

    // The model wants the waveform as fractions of full scale rather than
    // as the integers a microphone produces.
    final pcm = chunk.buffer.asInt16List(chunk.offsetInBytes, samples);
    final wave = Float32List(samples);
    for (var i = 0; i < samples; i++) {
      wave[i] = pcm[i] / 32768.0;
    }

    try {
      stream.acceptWaveform(samples: wave, sampleRate: _sampleRate);
      var decodes = 0;
      while (recogniser.isReady(stream)) {
        recogniser.decode(stream);
        decodes++;
      }

      final text = recogniser.getResult(stream).text.trim();
      final ended = recogniser.isEndpoint(stream);

      // Roughly once a second, and only while nothing is coming out. Says
      // whether audio is arriving, whether there is anything in it, and
      // whether the decoder is doing any work on it.
      _chunks++;
      for (final sample in wave) {
        final size = sample.abs();
        if (size > _loudest) _loudest = size;
      }
      if (_chunks % 16 == 0) {
        debugPrint(
          'FRIDAY: heard $_chunks chunks, loudest '
          '${_loudest.toStringAsFixed(3)}, $samples samples, '
          '$decodes decodes, endpoint $ended, text "$text"',
        );
        _loudest = 0;
      }

      if (ended) {
        recogniser.reset(stream);
        _said = '';

        // An endpoint with nothing in it is the room being quiet, not a
        // question ending. Reset and say nothing: telling the caller a
        // turn had ended would have it close and reopen for every silence.
        if (text.isEmpty) return;

        _onResult?.call(Heard(text, settled: true));
        _onDone?.call();
        return;
      }

      if (text != _said) {
        _said = text;
        if (text.isNotEmpty) _onResult?.call(Heard(text, settled: false));
      }
    } on Object catch (e) {
      debugPrint('FRIDAY: recogniser: $e');
    }
  }

  /// _resetStream : Throws away what the recogniser has heard so far,
  /// without stopping anything.
  void _resetStream() {
    final recogniser = _recogniser;
    final stream = _stream;
    if (recogniser == null || stream == null) return;
    recogniser.reset(stream);
    _said = '';
  }

  @override
  Future<void> stop() async => _resetStream();

  @override
  Future<void> cancel() async => _resetStream();

  @override
  Future<void> dispose() async {
    await _mic?.cancel();
    _mic = null;
    await _recorder.stop();
    await _recorder.dispose();

    _stream?.free();
    _stream = null;
    _recogniser?.free();
    _recogniser = null;
    _preparing.dispose();
  }
}

/// sherpaAvailable : Whether this platform has the native library.
bool get sherpaAvailable => Platform.isAndroid || Platform.isIOS;
