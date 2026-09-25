/// Opening a response whose body is read as it arrives.
///
/// This exists because the browser's EventSource cannot carry an
/// Authorization header, and the assistant's stream requires one. Putting the token
/// in the query string instead would write a credential into proxy logs and
/// browser history, so the web implementation uses fetch, which does take
/// headers and does expose the body as it arrives. The native
/// implementation reads a streamed HTTP response.
///
/// Both produce the same thing — bytes as they arrive — so the event parsing
/// above them is written once.
library;

import 'byte_source_stub.dart'
    if (dart.library.io) 'byte_source_io.dart'
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

  /// close : Releases whatever the implementation holds. A source is not
  /// usable afterwards.
  void close();
}

/// defaultByteSource : The implementation for the platform this is running
/// on.
ByteSource defaultByteSource() => platform.createByteSource();
