/// Streaming in a browser.
///
/// fetch rather than EventSource, because EventSource cannot send an
/// Authorization header and the assistant's stream requires one. The alternative
/// would be a token in the query string, which writes a credential into
/// proxy logs and browser history.
library;

import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'byte_source.dart';

/// createByteSource : Returns a source backed by fetch.
ByteSource createByteSource() => WebByteSource();

/// WebByteSource : Reads a fetch response body as it arrives.
class WebByteSource implements ByteSource {
  web.AbortController? _inFlight;

  @override
  Future<StreamedResponse> open(Uri url, Map<String, String> headers) async {
    final jsHeaders = web.Headers();
    // An explicit closure, not a tear-off: a JS interop member cannot be
    // torn off, and the analyzer does not catch it — only the web compiler
    // does.
    headers.forEach((name, value) => jsHeaders.append(name, value));

    // Abort is the only way to stop a fetch; without it, closing the client
    // would leave the connection open until the chat ended on its own.
    final controller = web.AbortController();
    _inFlight = controller;

    final response = await web.window
        .fetch(
          url.toString().toJS,
          web.RequestInit(
            method: 'GET',
            headers: jsHeaders,
            signal: controller.signal,
          ),
        )
        .toDart;

    return StreamedResponse(
      statusCode: response.status,
      body: _read(response),
      contentType: response.headers.get('content-type'),
    );
  }

  @override
  Future<StreamedResponse> post(
    Uri url,
    Map<String, String> headers,
    String body,
  ) async {
    final jsHeaders = web.Headers();
    headers.forEach((name, value) => jsHeaders.append(name, value));

    final controller = web.AbortController();
    _inFlight = controller;

    final response = await web.window
        .fetch(
          url.toString().toJS,
          web.RequestInit(
            method: 'POST',
            headers: jsHeaders,
            body: body.toJS,
            signal: controller.signal,
          ),
        )
        .toDart;

    return StreamedResponse(
      statusCode: response.status,
      body: _read(response),
      contentType: response.headers.get('content-type'),
    );
  }

  /// _read : Yields each chunk the body produces, until it ends or the
  /// listener stops caring.
  Stream<List<int>> _read(web.Response response) async* {
    final body = response.body;
    if (body == null) return;

    final reader = body.getReader() as web.ReadableStreamDefaultReader;
    try {
      while (true) {
        final chunk = await reader.read().toDart;
        if (chunk.done) return;
        final value = chunk.value;
        if (value.isDefinedAndNotNull) {
          yield (value! as JSUint8Array).toDart;
        }
      }
    } finally {
      // Reached when the listener cancels as well as when the body ends,
      // so a client that stops listening stops the download too.
      reader.cancel().toDart.ignore();
    }
  }

  @override
  void close() {
    _inFlight?.abort();
    _inFlight = null;
  }
}
