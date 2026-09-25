/// Keeping the token between runs.
library;

import 'package:shared_preferences/shared_preferences.dart';

import 'token_store.dart';

/// _key : Where the token is kept. Prefixed, because on the web this shares
/// a namespace with anything else served from the same origin.
const String _key = 'assistant.token';

/// StoredToken : A TokenStore backed by the platform's preferences.
///
/// App-private on Android and origin-scoped in a browser. Not encrypted at
/// rest: on Android that means anything with root can read it, and in a
/// browser that script running on the assistant's own origin can. The mitigation
/// is that the token is revocable from any other client rather than that it
/// is hidden — but on Android this should become the keystore, which is
/// noted where the Android work is planned.
class StoredToken implements TokenStore {
  @override
  Future<String?> read() async {
    final prefs = await SharedPreferences.getInstance();
    final token = prefs.getString(_key);
    return (token == null || token.isEmpty) ? null : token;
  }

  @override
  Future<void> write(String token) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, token);
  }

  @override
  Future<void> clear() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_key);
  }
}
