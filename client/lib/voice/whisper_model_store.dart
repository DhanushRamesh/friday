/// Keeping the Whisper model on disk.
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:whisper_ggml/whisper_ggml.dart';

/// ModelProgress : How far a download has got, as a fraction from zero to
/// one, or null while the size is unknown.
typedef ModelProgress = void Function(double? fraction);

/// WhisperModelStore : Finds the model file, fetching it the first time.
///
/// The model is not shipped in the APK. Half a gigabyte of weights would
/// have to be downloaded from the store on install and again on every
/// update, and most of that would be wasted on a device that never listens.
/// Fetching it on first use costs one wait and nothing afterwards.
class WhisperModelStore {
  WhisperModelStore({required this.model});

  /// model : Which set of weights to use.
  final WhisperModel model;

  /// _fetching : Held while a download is in progress, so two callers
  /// asking at once wait on one download rather than starting two.
  Future<String>? _fetching;

  /// path : Where the weights live, whether or not they are there yet.
  Future<String> path() async =>
      '${await WhisperController.getModelDir()}/ggml-${model.modelName}.bin';

  /// ready : Reports whether the weights are already on disk, so a caller
  /// can warn that the first use will wait.
  Future<bool> ready() async => File(await path()).exists();

  /// ensure : Returns the path to the weights, downloading them if they are
  /// not there. It throws if the download fails.
  Future<String> ensure({ModelProgress? onProgress}) {
    return _fetching ??= _fetch(onProgress).whenComplete(() {
      _fetching = null;
    });
  }

  Future<String> _fetch(ModelProgress? onProgress) async {
    final destination = await path();
    if (await File(destination).exists()) return destination;

    // Written beside the real name and renamed at the end. A download cut
    // off halfway would otherwise leave a file that looks like a model and
    // fails deep inside the decoder.
    final partial = File('$destination.part');
    await partial.parent.create(recursive: true);

    final client = http.Client();
    try {
      final response = await client.send(
        http.Request('GET', model.modelUri)..followRedirects = true,
      );
      if (response.statusCode != 200) {
        throw HttpException(
          'the model server answered ${response.statusCode}',
          uri: model.modelUri,
        );
      }

      final total = response.contentLength;
      var written = 0;
      final sink = partial.openWrite();
      try {
        await for (final chunk in response.stream) {
          sink.add(chunk);
          written += chunk.length;
          onProgress?.call(total == null ? null : written / total);
        }
      } finally {
        await sink.close();
      }

      await partial.rename(destination);
      debugPrint(
        'FRIDAY: fetched the ${model.modelName} model, $written bytes',
      );
      return destination;
    } on Object {
      if (await partial.exists()) await partial.delete();
      rethrow;
    } finally {
      client.close();
    }
  }
}
