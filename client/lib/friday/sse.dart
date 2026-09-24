/// Parsing the server-sent event stream a chat produces.
library;

import 'dart:async';
import 'dart:convert';

import 'errors.dart';
import 'models.dart';

/// _maxFrameBytes : The largest single event accepted before giving up.
///
/// A stream is read from a socket that something else may be writing to. A
/// frame that never ends would otherwise grow until the phone runs out of
/// memory, so it is bounded rather than trusted.
const int _maxFrameBytes = 1 << 20;

/// parseEvents : Turns a stream of bytes into the events it carries.
///
/// Implements as much of the server-sent events format as FRIDAY uses:
/// `event:`, `data:` and `id:` fields, a blank line dispatching the frame,
/// and `:` comments, which the server sends as heartbeats to stop a proxy
/// closing an idle connection. Fields the server does not send are ignored
/// rather than rejected.
///
/// The stream is decoded incrementally. A chunk boundary can fall anywhere,
/// including inside a multi-byte character, which is why the decoder is fed
/// rather than the whole body being collected first — collecting it would
/// also defeat the point, since the answer must be spoken as it arrives.
Stream<ChatEvent> parseEvents(Stream<List<int>> bytes) {
  late StreamController<ChatEvent> out;
  StreamSubscription<String>? sub;

  var buffer = StringBuffer();
  var data = StringBuffer();
  var pending = false;

  void reset() {
    data = StringBuffer();
    pending = false;
  }

  void dispatch() {
    if (!pending) {
      reset();
      return;
    }
    final payload = data.toString();
    reset();

    final Map<String, dynamic> decoded;
    try {
      decoded = jsonDecode(payload) as Map<String, dynamic>;
    } on FormatException catch (e) {
      out.addError(
        BadResponse(
          'FRIDAY sent an event I could not read.',
          body: '$payload ($e)',
        ),
      );
      return;
    }
    out.add(ChatEvent.fromJson(decoded));
  }

  void handleLine(String line) {
    if (line.isEmpty) {
      dispatch();
      return;
    }
    // A comment. The server sends ": keep-alive" on an idle stream.
    if (line.startsWith(':')) return;

    final colon = line.indexOf(':');
    final field = colon < 0 ? line : line.substring(0, colon);
    var value = colon < 0 ? '' : line.substring(colon + 1);
    // One optional space after the colon belongs to the format, not the value.
    if (value.startsWith(' ')) value = value.substring(1);

    switch (field) {
      case 'data':
        // The format allows a value to span several data lines, joined by
        // newlines. FRIDAY sends one, but a conforming parser accepts both.
        if (pending) data.write('\n');
        data.write(value);
        pending = true;
      case 'event':
      case 'id':
      case 'retry':
        // All three repeat something already in the payload or unused: the
        // event name repeats its kind, the identifier its sequence number,
        // and FRIDAY never asks for a different retry interval. Parsed so
        // that they cannot be mistaken for data, then dropped.
        break;
      default:
        break;
    }
  }

  void handleChunk(String chunk) {
    buffer.write(chunk);
    var text = buffer.toString();

    if (text.length > _maxFrameBytes) {
      out.addError(const BadResponse('FRIDAY sent an event that never ended.'));
      sub?.cancel();
      out.close();
      return;
    }

    var start = 0;
    while (true) {
      final nl = text.indexOf('\n', start);
      if (nl < 0) break;
      var line = text.substring(start, nl);
      // Tolerate CRLF, which a proxy may introduce.
      if (line.endsWith('\r')) line = line.substring(0, line.length - 1);
      handleLine(line);
      start = nl + 1;
    }

    buffer = StringBuffer(text.substring(start));
  }

  out = StreamController<ChatEvent>(
    onListen: () {
      sub = utf8.decoder
          .bind(bytes)
          .listen(
            handleChunk,
            onError: out.addError,
            onDone: () {
              // A stream that ends mid-frame drops it: a partial event is
              // not an event.
              out.close();
            },
            cancelOnError: true,
          );
    },
    onCancel: () => sub?.cancel(),
  );
  return out.stream;
}
