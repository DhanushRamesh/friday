/// Where a token is kept between runs.
library;

/// TokenStore : Somewhere to keep the bearer token.
///
/// An interface rather than a concrete store because the right answer
/// differs per platform — the Android keystore, the browser's storage — and
/// because a test wants neither. The client holds the token in memory once
/// read; this is only about surviving a restart.
abstract interface class TokenStore {
  /// read : Returns the stored token, or null if there is none.
  Future<String?> read();

  /// write : Replaces the stored token.
  Future<void> write(String token);

  /// clear : Removes it. Called on logout and whenever the server refuses
  /// the token, since a refused token never becomes valid again.
  Future<void> clear();
}

/// InMemoryTokenStore : Keeps the token for as long as the process runs.
///
/// The default, so that the client works before a platform store is wired
/// in. Its cost is that the user logs in again on every start.
class InMemoryTokenStore implements TokenStore {
  String? _token;

  @override
  Future<String?> read() async => _token;

  @override
  Future<void> write(String token) async => _token = token;

  @override
  Future<void> clear() async => _token = null;
}
