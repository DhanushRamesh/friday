/// Silencing the noise the recogniser makes when it starts.
library;

/// HushingTheTone : Silences whatever the platform plays when a recognition
/// session begins.
///
/// An interface, because only Android makes the noise. A browser's
/// recogniser is silent and so is every desktop.
abstract interface class HushingTheTone {
  /// around : Runs [opening] with the tone silenced.
  ///
  /// Takes the work rather than offering mute and unmute separately: a
  /// caller that forgets to unmute leaves the phone silent with nothing on
  /// screen to explain it, and that is too easy to forget.
  Future<T> around<T>(Future<T> Function() opening);
}

/// NoToneToHush : For the platforms that make no noise.
class NoToneToHush implements HushingTheTone {
  const NoToneToHush();

  @override
  Future<T> around<T>(Future<T> Function() opening) => opening();
}
