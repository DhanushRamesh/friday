/// Streaming on Android, iOS and desktop, where dart:io is available.
library;

import 'package:http/http.dart' as http;

import 'byte_source.dart';

/// createByteSource : Returns a source backed by a reusable HTTP client.
ByteSource createByteSource() => IoByteSource();

/// IoByteSource : Reads a streamed HTTP response.
class IoByteSource implements ByteSource {
  IoByteSource([http.Client? client]) : _client = client ?? http.Client();

  final http.Client _client;

  @override
  Future<StreamedResponse> open(Uri url, Map<String, String> headers) async {
    final request = http.Request('GET', url)..headers.addAll(headers);
    // send, not get: get reads the whole body before returning, which for a
    // stream that stays open until the chat ends would never return at all.
    final response = await _client.send(request);
    return StreamedResponse(
      statusCode: response.statusCode,
      body: response.stream,
      contentType: response.headers['content-type'],
    );
  }

  @override
  void close() => _client.close();
}
