/// Keeping the microphone alive when the screen is not.
library;

/// StayingAwake : Holds whatever the platform needs in order to keep
/// listening while the app is not in front of the user.
///
/// An interface, because only Android needs anything: a browser tab that is
/// not visible has already lost the microphone and cannot ask for it back,
/// and a desktop never loses it.
abstract interface class StayingAwake {
  /// begin : Starts holding it, reporting why it could not if it could not.
  ///
  /// A sentence rather than a flag: the reasons a phone refuses are
  /// different from each other and the user can act on each — a refused
  /// notification permission is not the same as a service Android would
  /// not start.
  Future<String?> begin();

  /// end : Releases it. Safe to call when nothing is held.
  Future<void> end();
}

/// NothingToHold : For the platforms that need nothing.
class NothingToHold implements StayingAwake {
  const NothingToHold();

  @override
  Future<String?> begin() async => null;

  @override
  Future<void> end() async {}
}
