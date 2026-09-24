import 'package:flutter_test/flutter_test.dart';
import 'package:friday_client/friday/server_url.dart';

const _dev = 'http://192.168.0.105:8080';
const _prod = 'https://friday-server.duckdns.org';

Uri choose({
  String override = '',
  String development = _dev,
  Uri? pageOrigin,
  required bool debug,
}) => chooseServerUrl(
  override: override,
  development: development,
  deployed: _prod,
  pageOrigin: pageOrigin,
  debug: debug,
);

void main() {
  group('choosing the server', () {
    test('a debug build on a phone talks to the machine on the network', () {
      expect(choose(debug: true).toString(), _dev);
    });

    // The one that matters. An installed app must not be pointed at a laptop
    // that will not be on the network tomorrow, however it was built.
    test('a release build never reaches for the development server', () {
      expect(choose(debug: false).toString(), _prod);
    });

    test(
      'a debug build with no development server falls back to the deployed one',
      () {
        expect(choose(development: '', debug: true).toString(), _prod);
      },
    );

    test('an explicit override wins in either mode', () {
      const elsewhere = 'http://10.0.0.7:9000';
      expect(choose(override: elsewhere, debug: true).toString(), elsewhere);
      expect(choose(override: elsewhere, debug: false).toString(), elsewhere);
    });

    // In the browser FRIDAY serves the page, so its own origin is the answer
    // and the path the router left behind is not part of it.
    test('the web build talks to whoever served the page', () {
      final origin = Uri.parse('https://friday-server.duckdns.org/chat?x=1#y');
      expect(
        choose(pageOrigin: origin, debug: false).toString(),
        'https://friday-server.duckdns.org',
      );
    });

    test('an override still beats the page origin, for web development', () {
      expect(
        choose(
          override: _dev,
          pageOrigin: Uri.parse('http://localhost:5100/'),
          debug: true,
        ).toString(),
        _dev,
      );
    });
  });
}
