/// What this install calls itself, kept between sign-ins.
library;

import 'package:shared_preferences/shared_preferences.dart';

/// _key : Where the name is kept. Prefixed, because on the web this shares a
/// namespace with anything else served from the same origin.
const String _key = 'assistant.client_name';

/// RememberedClient : The name this browser registered itself under.
///
/// Kept separately from the token and deliberately outlives it. Signing out
/// takes the token away; it does not make this a different browser, and being
/// asked to name it again would suggest otherwise — and invite a second name
/// for the same thing, which is exactly what makes a listing of clients
/// useless.
///
/// Not a secret: it is a label the person chose, and anything that can read it
/// could read the token beside it.
class RememberedClient {
  /// read : The name from a previous sign-in, or null if there was none.
  Future<String?> read() async {
    final prefs = await SharedPreferences.getInstance();
    final name = prefs.getString(_key);
    return (name == null || name.isEmpty) ? null : name;
  }

  /// write : Remembers the name this browser signed in under.
  Future<void> write(String name) async {
    final trimmed = name.trim();
    if (trimmed.isEmpty) return;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, trimmed);
  }

  /// forget : Drops the name, so the next sign-in asks again.
  ///
  /// Signing out does not do this. It is for someone who wants to call this
  /// browser something else.
  Future<void> forget() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_key);
  }
}
