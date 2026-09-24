import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/voice/hushing_the_tone.dart';

/// _Records : A hush that writes down what it was asked to do.
class _Records implements HushingTheTone {
  final events = <String>[];

  @override
  Future<T> around<T>(Future<T> Function() opening) async {
    events.add('quiet');
    try {
      return await opening();
    } finally {
      events.add('loud');
    }
  }
}

void main() {
  group('hushing the tone', () {
    test('a platform with no tone just runs the work', () async {
      const hush = NoToneToHush();
      var ran = false;
      final got = await hush.around(() async {
        ran = true;
        return 7;
      });

      expect(ran, isTrue);
      expect(got, 7);
    });

    test('the sound comes back even when opening fails', () async {
      final hush = _Records();

      await expectLater(
        hush.around(() async => throw StateError('recogniser is busy')),
        throwsStateError,
      );

      // The one that matters. A phone left muted with nothing on screen to
      // explain it is worse than the tone ever was.
      expect(hush.events, ['quiet', 'loud']);
    });

    test('the work runs between the two', () async {
      final hush = _Records();
      await hush.around(() async => hush.events.add('listen'));
      expect(hush.events, ['quiet', 'listen', 'loud']);
    });
  });
}
