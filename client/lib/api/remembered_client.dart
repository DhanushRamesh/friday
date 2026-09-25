/// What this install calls itself, kept between sign-ins.
library;

import 'package:shared_preferences/shared_preferences.dart';

/// _nameKey, _idKey : Where these are kept. Prefixed, because on the web this
/// shares a namespace with anything else served from the same origin.
const String _nameKey = 'assistant.client_name';
const String _idKey = 'assistant.client_id';

/// RememberedClient : What this browser registered itself as.
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
  /// name : What this browser called itself, or null if it never has.
  Future<String?> name() async => _read(_nameKey);

  /// id : The client this browser registered as, or null.
  ///
  /// Presented when signing in again, so the server re-issues that client's
  /// token instead of registering another. Not a secret: it names a client,
  /// and the password is what authorises using it.
  Future<String?> id() async => _read(_idKey);

  /// remember : Keeps what the server registered, after it accepted the
  /// sign-in. Before that there is nothing to keep: a name from a failed
  /// attempt is a name nothing is registered under.
  Future<void> remember({required String id, required String name}) async {
    final prefs = await SharedPreferences.getInstance();
    if (id.isNotEmpty) await prefs.setString(_idKey, id);
    if (name.trim().isNotEmpty) await prefs.setString(_nameKey, name.trim());
  }

  /// forget : Drops both, so the next sign-in asks again and registers a new
  /// client.
  ///
  /// Signing out does not do this. It is for someone who wants this browser
  /// to be a different client.
  Future<void> forget() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_nameKey);
    await prefs.remove(_idKey);
  }

  Future<String?> _read(String key) async {
    final prefs = await SharedPreferences.getInstance();
    final value = prefs.getString(key);
    return (value == null || value.isEmpty) ? null : value;
  }
}
