import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/voice/endpointer.dart';

const int _rate = 16000;

/// _chunk : A block of audio of the given length and loudness, as a sine
/// wave so the RMS is predictable.
Uint8List _chunk(Duration span, double amplitude) {
  final samples = span.inMicroseconds * _rate ~/ 1000000;
  final pcm = Int16List(samples);
  for (var i = 0; i < samples; i++) {
    // sqrt(2) so that a wave of this amplitude has an RMS of `amplitude`.
    final value =
        math.sin(2 * math.pi * 220 * i / _rate) *
        amplitude *
        math.sqrt2 *
        32767;
    pcm[i] = value.round().clamp(-32768, 32767);
  }
  return pcm.buffer.asUint8List();
}

/// _speech : Loud enough to be unmistakably someone talking.
Uint8List _speech(Duration span) => _chunk(span, 0.2);

/// _room : A quiet room — not digital silence, which no microphone ever
/// produces.
Uint8List _room(Duration span) => _chunk(span, 0.0002);

void main() {
  group('endpointer', () {
    test('says nothing while someone is talking', () {
      final ears = Endpointer(sampleRate: _rate);

      for (var i = 0; i < 40; i++) {
        final ending = ears.hear(_speech(const Duration(milliseconds: 100)));
        expect(ending, Ending.none);
      }
      expect(ears.spoke, isTrue);
    });

    test('ends the stretch once the speaker stops', () {
      final ears = Endpointer(
        sampleRate: _rate,
        pauseAfterSpeech: const Duration(milliseconds: 500),
      );

      ears.hear(_speech(const Duration(milliseconds: 300)));

      // Four hundred milliseconds of quiet is a pause, not an ending.
      expect(ears.hear(_room(const Duration(milliseconds: 400))), Ending.none);
      expect(ears.hear(_room(const Duration(milliseconds: 200))), Ending.spoke);
    });

    // The bug that made this worth extracting: a pause mid-sentence must
    // not be read as the end of the turn.
    test('a pause between words does not end the stretch', () {
      final ears = Endpointer(
        sampleRate: _rate,
        pauseAfterSpeech: const Duration(milliseconds: 1200),
      );

      ears.hear(_speech(const Duration(milliseconds: 500)));
      expect(ears.hear(_room(const Duration(milliseconds: 900))), Ending.none);
      expect(
        ears.hear(_speech(const Duration(milliseconds: 500))),
        Ending.none,
      );
      expect(ears.hear(_room(const Duration(milliseconds: 900))), Ending.none);
      expect(
        ears.hear(_speech(const Duration(milliseconds: 500))),
        Ending.none,
      );
    });

    // Silence has to be reported too, or always-awake would sit on an open
    // microphone in an empty room for ever.
    test('gives up when nobody says anything', () {
      final ears = Endpointer(
        sampleRate: _rate,
        patienceBeforeSpeech: const Duration(seconds: 2),
      );

      expect(ears.hear(_room(const Duration(milliseconds: 1500))), Ending.none);
      expect(
        ears.hear(_room(const Duration(milliseconds: 600))),
        Ending.silence,
      );
      expect(ears.spoke, isFalse);
    });

    // A quiet talker must not have to shout over the bar the room set. The
    // owner's words, of the recogniser this replaces: "i should not shout".
    test('a soft voice in a quiet room is heard', () {
      final ears = Endpointer(sampleRate: _rate);

      // Let the floor settle to a quiet room first.
      for (var i = 0; i < 20; i++) {
        ears.hear(_room(const Duration(milliseconds: 100)));
      }
      expect(ears.noiseFloor, lessThan(0.01));

      expect(
        ears.hear(_chunk(const Duration(milliseconds: 100), 0.01)),
        Ending.none,
      );
      expect(ears.spoke, isTrue);
    });

    // A fan or a fridge raises the floor, so the bar for speech rises with
    // it rather than the hum being transcribed as talking.
    test('a noisy room does not count as talking', () {
      final ears = Endpointer(
        sampleRate: _rate,
        patienceBeforeSpeech: const Duration(seconds: 1),
      );

      // Hum at the cap: loud for a room, quiet for a voice.
      var ending = Ending.none;
      for (var i = 0; i < 12; i++) {
        ending = ears.hear(_chunk(const Duration(milliseconds: 100), 0.008));
      }
      expect(ears.spoke, isFalse);
      expect(ending, Ending.silence);
    });

    test('an empty chunk is not an ending', () {
      final ears = Endpointer(sampleRate: _rate);
      expect(ears.hear(Uint8List(0)), Ending.none);
    });
  });
}
