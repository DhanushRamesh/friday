/// Fetching a model file the first time it is needed.
library;

import 'dart:io';

import 'package:http/http.dart' as http;

/// Progress : How far a download has got, as a fraction from zero to one,
/// or null while the size is unknown.
typedef Progress = void Function(double? fraction);

/// fetchTo : Downloads [from] to [to], unless it is already there.
///
/// Written beside the real name and renamed at the end. A download cut off
/// halfway would otherwise leave a file that looks like a model and fails
/// deep inside whatever tries to load it.
Future<void> fetchTo(Uri from, String to, {Progress? onProgress}) async {
  final destination = File(to);
  if (await destination.exists()) return;

  final partial = File('$to.part');
  await partial.parent.create(recursive: true);

  final client = http.Client();
  try {
    final response = await client.send(
      http.Request('GET', from)..followRedirects = true,
    );
    if (response.statusCode != 200) {
      throw HttpException(
        'the model server answered ${response.statusCode}',
        uri: from,
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

    await partial.rename(to);
  } on Object {
    if (await partial.exists()) await partial.delete();
    rethrow;
  } finally {
    client.close();
  }
}
