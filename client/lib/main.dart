/// A thin web client for the personal assistant server.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'api/stored_token.dart';
import 'design/design.dart';
import 'screens/home.dart';
import 'screens/login.dart';
import 'state/app_state.dart';

void main() {
  runApp(
    ClientApp(
      state: AppState(
        api: AssistantApi(baseUrl: resolveServerUrl(), tokens: StoredToken()),
      ),
    ),
  );
}

/// ClientApp : The root. Holds the state and decides which screen is showing.
class ClientApp extends StatefulWidget {
  const ClientApp({super.key, required this.state});

  final AppState state;

  @override
  State<ClientApp> createState() => _ClientAppState();
}

class _ClientAppState extends State<ClientApp> {
  /// _started : Whether the attempt to reuse a kept token has finished.
  ///
  /// Until it has, neither screen is right: showing the login would make
  /// someone who is already signed in watch it flash past.
  bool _started = false;

  @override
  void initState() {
    super.initState();
    widget.state.start().whenComplete(() {
      if (mounted) setState(() => _started = true);
    });
  }

  @override
  void dispose() {
    widget.state.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Assistant',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      home: AnimatedBuilder(
        animation: widget.state,
        builder: (context, _) {
          if (!_started) return const _Starting();
          return widget.state.signedIn
              ? HomeScreen(state: widget.state)
              : LoginScreen(state: widget.state);
        },
      ),
    );
  }
}

/// _Starting : What is shown while the kept token is being checked.
class _Starting extends StatelessWidget {
  const _Starting();

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: context.colors.background,
    body: const Center(child: AppSpinner(size: 24)),
  );
}
