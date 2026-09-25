/// What can go wrong talking to the assistant.
library;

/// ApiException : The base of every failure this client reports.
///
/// A caller that only wants to show something to the user can catch this and
/// read [message], which is always phrased for a person and may be spoken
/// aloud. The subtypes exist for the decisions a caller actually makes:
/// send them back to the login screen, retry, or give up.
sealed class ApiException implements Exception {
  const ApiException(this.message);

  /// message : What went wrong, in plain language.
  final String message;

  @override
  String toString() => '$runtimeType: $message';
}

/// NotAuthenticated : There is no usable token, or the server refused the
/// one presented.
///
/// The only recovery is to log in again: a token is never renewed, and a
/// revoked one never becomes valid.
final class NotAuthenticated extends ApiException {
  const NotAuthenticated([super.message = 'You need to log in again.']);
}

/// NotFound : The thing asked for does not exist, or belongs to someone else.
///
/// The server does not distinguish the two on purpose, so neither does this.
final class NotFound extends ApiException {
  const NotFound([super.message = 'That does not exist.']);
}

/// Refused : The request was wrong and repeating it unchanged will fail
/// again — a prompt that is empty or too long, a malformed identifier.
final class Refused extends ApiException {
  const Refused(super.message, {this.statusCode = 400});

  /// statusCode : What the server answered.
  final int statusCode;
}

/// Conflict : The request no longer applies, such as cancelling a chat that
/// has already finished.
final class Conflict extends ApiException {
  const Conflict([super.message = 'That has already finished.']);
}

/// ServerFailure : the assistant failed on its side. The cause is in its log, not
/// in this message, by design.
final class ServerFailure extends ApiException {
  const ServerFailure(super.message, {required this.statusCode});

  /// statusCode : What the server answered.
  final int statusCode;

  /// isRetryable : Whether trying again later could work. A 503 means the assistant
  /// is not accepting work at the moment, which passes.
  bool get isRetryable => statusCode == 503 || statusCode == 502;
}

/// Unreachable : the assistant could not be contacted at all — no network, no DNS,
/// a refused connection, or a timeout.
///
/// This is the one failure that is expected in normal use, since the client
/// is a phone that moves between networks.
final class Unreachable extends ApiException {
  const Unreachable([
    super.message = 'I cannot reach the assistant at the moment.',
    this.cause,
  ]);

  /// cause : The underlying error, for the log rather than the user.
  final Object? cause;
}

/// BadResponse : The server answered with something this client cannot
/// parse, which means the two disagree about the contract.
final class BadResponse extends ApiException {
  const BadResponse(super.message, {this.body});

  /// body : What arrived, truncated, for diagnosis.
  final String? body;
}
