/// Opening a response whose body is read as it arrives.
///
/// A prompt is answered as the words are produced, so the response has to be
/// read while it is still arriving rather than after it ends. The web
/// implementation uses fetch, which exposes the body as it comes and takes an
/// Authorization header; the native one reads a streamed HTTP response.
///
/// Both produce the same thing — bytes as they arrive — so the parsing above
/// them is written once.
library;

// The stub is the fallback, and it is also what dart:io gets: there is no
// native implementation yet, and naming a file that does not exist made the
// whole client fail to compile for anything but the browser, which is why
// `flutter test` could not run at all.
import 'byte_source_stub.dart'
    if (dart.library.js_interop) 'byte_source_web.dart'
    as platform;

/// StreamedResponse : A response whose body has not been read yet.
class StreamedResponse {
  const StreamedResponse({
    required this.statusCode,
    required this.body,
    this.contentType,
  });

  final int statusCode;

  /// body : The bytes as they arrive. For a failing status this is the error
  /// body, which is short and can simply be collected.
  final Stream<List<int>> body;

  final String? contentType;
}

/// ByteSource : Opens a request and hands back its body unread.
abstract interface class ByteSource {
  /// open : Issues a GET and returns as soon as the headers arrive, before
  /// the body has been read.
  Future<StreamedResponse> open(Uri url, Map<String, String> headers);

  /// post : The same, with a body. This is how a prompt is sent: the one
  /// endpoint that takes one answers as the words are produced, so the
  /// response has to be read while it is still arriving.
  Future<StreamedResponse> post(
    Uri url,
    Map<String, String> headers,
    String body,
  );

  /// close : Releases whatever the implementation holds. A source is not
  /// usable afterwards.
  void close();
}

/// defaultByteSource : The implementation for the platform this is running
/// on.
ByteSource defaultByteSource() => platform.createByteSource();
