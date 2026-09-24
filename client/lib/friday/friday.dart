/// A client for FRIDAY's HTTP interface.
///
/// Nothing here depends on Flutter, so it can be exercised by a plain Dart
/// test and reused by anything that is not a widget.
///
/// Start with [FridayApi]: it holds the server address and the bearer token,
/// and turns every failure into one of the types in `errors.dart`.
library;

export 'api.dart' show FridayApi;
export 'byte_source.dart' show ByteSource, StreamedResponse;
export 'errors.dart';
export 'models.dart'
    show
        Client,
        EventKind,
        Identity,
        LoginResult,
        Message,
        Session,
        SessionDetail,
        Chat,
        ChatEvent,
        ChatStatus,
        ChatSummary,
        User;
export 'sse.dart' show parseEvents;
export 'token_store.dart';
