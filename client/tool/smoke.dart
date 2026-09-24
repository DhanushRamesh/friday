// A run against a real FRIDAY, to check the client against the server rather
// than against a fake of it. Not part of the test suite: it needs a running
// server, an account, and a provider that costs money to call.
//
//   dart run tool/smoke.dart http://127.0.0.1:8080 <username> <password>
import 'dart:io';

import 'package:friday_client/friday/friday.dart';

Future<void> main(List<String> args) async {
  if (args.length < 3) {
    stderr.writeln('usage: smoke.dart <base-url> <username> <password>');
    exit(2);
  }
  final api = FridayApi(baseUrl: Uri.parse(args[0]));

  try {
    final login = await api.login(
      username: args[1],
      password: args[2],
      clientName: 'smoke test',
    );
    say('logged in as ${login.user.username}, client ${login.client.id}');
    say('active session ${login.client.activeSessionId}');

    final me = await api.me();
    say('me: ${me.user.username} from ${me.client.name}');

    final task = await api.createTask('In one short sentence, what is Go?');
    say('task ${task.id} is ${task.status.wire}');

    var heard = 0;
    await for (final event in api.streamTask(task.id)) {
      heard++;
      say(
        '  ${event.kind.wire} ${event.seq > 0 ? '#${event.seq} ' : ''}'
        '${event.text}',
      );
    }
    say('stream ended after $heard events');

    final finished = await api.task(task.id);
    say('final status ${finished.status.wire}');
    if (finished.response.isNotEmpty) say('answer: ${finished.response}');

    final messages = await api.messages(task.id);
    say('${messages.length} messages stored');

    final sessions = await api.listSessions(limit: 3);
    say('${sessions.length} sessions');

    final detail = await api.session(task.sessionId);
    say('session holds ${detail.tasks.length} tasks');

    // Resume must not replay what was already heard.
    if (messages.isNotEmpty) {
      final resumed = await api
          .streamTask(task.id, resumeFrom: messages.last.seq)
          .toList();
      final replayed = resumed.where(
        (e) => e.seq > 0 && e.seq <= messages.last.seq,
      );
      say(
        'resume from ${messages.last.seq}: ${resumed.length} events, '
        '${replayed.length} replayed (want 0)',
      );
    }
  } on FridayException catch (e) {
    stderr.writeln('FAILED: $e');
    exit(1);
  } finally {
    api.close();
  }
}

void say(String line) => stdout.writeln(line);
