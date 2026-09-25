/// Where the assistant is.
library;

import 'package:flutter/foundation.dart' show kDebugMode, kIsWeb;

/// _override : Set at build time with
/// `--dart-define=ASSISTANT_URL=http://192.168.0.105:8080`.
///
/// It wins everywhere, debug or release. Needed during web development,
/// where `flutter run` serves the UI from its own port so the page's own
/// address is the dev server rather than the assistant, and for pointing a build
/// at something neither of the two defaults covers.
const String _override = String.fromEnvironment('ASSISTANT_URL');

/// _development : The server a debug build talks to, set at build time.
///
/// The Makefile fills this in with the building machine's own address on
/// the network, which is what a phone on the same wifi can reach.
/// `localhost` on a phone means the phone itself, which is nothing at all.
const String _development = String.fromEnvironment('ASSISTANT_DEV_URL');

/// _deployed : Where the assistant answers from, and the only address a release
/// build will ever use.
const String _deployed = 'https://friday-server.duckdns.org';

/// resolveServerUrl : Returns the address to talk to.
///
/// A debug build looks for the development server and a release build never
/// does, so an app built to be installed cannot end up pointed at a laptop
/// that will not be on the network tomorrow. That is decided here rather
/// than by how the build was invoked, because forgetting a flag should not
/// be able to ship a broken app.
///
/// In production the web bundle is served by the assistant itself, so the page's
/// own origin is the right answer and nothing has to be configured.
Uri resolveServerUrl() => chooseServerUrl(
  override: _override,
  development: _development,
  deployed: _deployed,
  pageOrigin: kIsWeb ? Uri.base : null,
  debug: kDebugMode,
);

/// chooseServerUrl : The decision on its own, so it can be tested.
///
/// The build-mode constants it reads are fixed at compile time, which means a
/// test run can only ever observe one of the answers. Taking them as
/// arguments is what lets the rule that matters — a release build never
/// reaches for the development server — be checked rather than assumed.
///
/// pageOrigin is the page's own address when running in a browser, and null
/// everywhere else.
Uri chooseServerUrl({
  required String override,
  required String development,
  required String deployed,
  required Uri? pageOrigin,
  required bool debug,
}) {
  if (override.isNotEmpty) return Uri.parse(override);
  if (pageOrigin != null) {
    // Built rather than stripped: `replace` cannot clear a query or a
    // fragment, and asking it for empty ones leaves "?#" on the end.
    return Uri(
      scheme: pageOrigin.scheme,
      host: pageOrigin.host,
      port: pageOrigin.hasPort ? pageOrigin.port : null,
    );
  }
  if (debug && development.isNotEmpty) return Uri.parse(development);
  return Uri.parse(deployed);
}
