import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/friday/friday.dart';

/// bytes : Turns whole strings into a stream, one chunk per string.
///
/// Each string is one chunk, so a test can put the split exactly where it
/// wants it — which is the point, since a chunk boundary is where this kind
/// of parser breaks.
Stream<List<int>> bytes(List<String> chunks) =>
    Stream.fromIterable(chunks.map(utf8.encode));

/// frame : One well-formed event, as the server writes it.
String frame(String kind, {int? seq, String text = ''}) {
  final payload = jsonEncode({
    'kind': kind,
    'seq': ?seq,
    if (text.isNotEmpty) 'text': text,
    'at': '2026-09-21T10:00:00Z',
  });
  return '${seq != null ? 'id: $seq\n' : ''}event: $kind\ndata: $payload\n\n';
}

void main() {
  test('reads the updates then the answer', () async {
    final got = await parseEvents(
      bytes([
        frame('update', seq: 1, text: 'Let me take a look.'),
        frame('update', seq: 2, text: 'Still working on it.'),
        frame('final', seq: 3, text: 'Here is the answer.'),
      ]),
    ).toList();

    expect(got.map((e) => e.kind), [
      EventKind.update,
      EventKind.update,
      EventKind.finalAnswer,
    ]);
    expect(got.map((e) => e.seq), [1, 2, 3]);
    expect(got.last.text, 'Here is the answer.');
    expect(got.last.isTerminal, isTrue);
    expect(got.first.isTerminal, isFalse);
  });

  // A chunk arrives when the network hands one over, which has nothing to do
  // with where an event ends. Splitting mid-field, mid-frame and mid-payload
  // must all produce the same events.
  test('survives a split anywhere in the stream', () async {
    final whole =
        frame('update', seq: 1, text: 'hello') +
        frame('final', seq: 2, text: 'goodbye');

    for (var at = 1; at < whole.length; at++) {
      final got = await parseEvents(
        bytes([whole.substring(0, at), whole.substring(at)]),
      ).toList();

      expect(got.length, 2, reason: 'split at $at');
      expect(got[0].text, 'hello', reason: 'split at $at');
      expect(got[1].text, 'goodbye', reason: 'split at $at');
    }
  });

  // A multi-byte character split across chunks must not become two broken
  // ones. An answer may well contain them.
  test('survives a split inside a multi-byte character', () async {
    final whole = utf8.encode(frame('final', seq: 1, text: 'café ☕'));

    // The é and the ☕ are multi-byte; try every boundary.
    for (var at = 1; at < whole.length; at++) {
      final got = await parseEvents(
        Stream.fromIterable([whole.sublist(0, at), whole.sublist(at)]),
      ).toList();

      expect(got.single.text, 'café ☕', reason: 'split at byte $at');
    }
  });

  // The server sends these on an idle stream so a proxy does not close it.
  // They carry nothing and must not appear as events.
  test('ignores heartbeat comments', () async {
    final got = await parseEvents(
      bytes([
        ': keep-alive\n\n',
        ': keep-alive\n\n',
        frame('final', seq: 1, text: 'done'),
      ]),
    ).toList();

    expect(got.single.text, 'done');
  });

  test('tolerates CRLF, which a proxy may introduce', () async {
    final crlf = frame('final', seq: 1, text: 'done').replaceAll('\n', '\r\n');

    final got = await parseEvents(bytes([crlf])).toList();
    expect(got.single.text, 'done');
  });

  // The format allows a value to span several data lines, joined with
  // newlines. FRIDAY sends one, but the parser should not depend on that.
  test('joins a payload split across data lines', () async {
    // Split between tokens, where the joining newline is only whitespace.
    const head = '{"kind":"final","seq":1,';
    const tail = '"text":"two lines","at":"2026-09-21T10:00:00Z"}';

    final got = await parseEvents(
      bytes(['event: final\ndata: $head\ndata: $tail\n\n']),
    ).toList();

    expect(got.single.text, 'two lines');
    expect(got.single.seq, 1);
  });

  test('accepts a field with no space after the colon', () async {
    final payload = jsonEncode({
      'kind': 'final',
      'seq': 1,
      'text': 'terse',
      'at': '2026-09-21T10:00:00Z',
    });

    final got = await parseEvents(
      bytes(['event:final\ndata:$payload\n\n']),
    ).toList();
    expect(got.single.text, 'terse');
  });

  test('reports an unreadable payload rather than dropping it', () async {
    final stream = parseEvents(bytes(['event: final\ndata: not json\n\n']));
    await expectLater(stream, emitsError(isA<BadResponse>()));
  });

  // A connection dropped mid-frame leaves a partial event, which is not an
  // event. It must not be delivered half-parsed.
  test('drops a frame the stream ended in the middle of', () async {
    final got = await parseEvents(
      bytes([
        frame('update', seq: 1, text: 'complete'),
        'event: final\ndata: {"kind":"fin',
      ]),
    ).toList();

    expect(got.single.text, 'complete');
  });

  test('passes on a transport failure', () async {
    final stream = parseEvents(Stream<List<int>>.error(const SocketishError()));
    await expectLater(stream, emitsError(isA<SocketishError>()));
  });

  // An unknown kind must not end the stream: hanging up on something
  // unrecognised would lose the answer that came after it.
  test('keeps listening through an unknown kind', () async {
    final got = await parseEvents(
      bytes([
        'event: thinking\n'
            'data: {"kind":"thinking","at":"2026-09-21T10:00:00Z"}\n\n',
        frame('final', seq: 1, text: 'the answer'),
      ]),
    ).toList();

    expect(got.first.kind, EventKind.unknown);
    expect(got.first.isTerminal, isFalse);
    expect(got.last.text, 'the answer');
  });

  test('stops reading when the listener cancels', () async {
    final controller = StreamController<List<int>>();
    var cancelled = false;
    controller.onCancel = () => cancelled = true;

    final sub = parseEvents(controller.stream).listen((_) {});
    controller.add(utf8.encode(frame('update', seq: 1, text: 'one')));
    await Future<void>.delayed(Duration.zero);

    await sub.cancel();
    expect(cancelled, isTrue);
  });
}

/// SocketishError : Stands in for whatever the transport throws.
class SocketishError implements Exception {
  const SocketishError();
}
