/// FRIDAY's client.
library;

import 'dart:async';

import 'package:flutter/foundation.dart' show kReleaseMode;
import 'package:flutter/material.dart';

import 'design/design.dart';
import 'design/gallery.dart';
import 'friday/friday.dart';
import 'friday/server_url.dart';
import 'friday/stored_token.dart';
import 'screens/chat.dart';
import 'screens/login.dart';
import 'state/conversation.dart';
import 'state/voice_session.dart';
import 'voice/voice.dart';

void main() {
  runApp(
    FridayApp(
      api: FridayApi(baseUrl: resolveServerUrl(), tokens: StoredToken()),
    ),
  );
}

/// FridayApp : The application, and the one place the theme is set.
class FridayApp extends StatelessWidget {
  const FridayApp({super.key, required this.api});

  final FridayApi api;

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'FRIDAY',
    debugShowCheckedModeBanner: false,
    theme: FTheme.light,
    darkTheme: FTheme.dark,
    // Dark unless the system asks otherwise: FRIDAY is used at night and on
    // a phone, and a long answer on a white field glares.
    themeMode: ThemeMode.dark,
    home: _Root(api: api),
    routes: {
      // Development only. It is how the design system is looked at, and it
      // has no place in a shipped build.
      if (!kReleaseMode) '/gallery': (_) => const FGallery(),
    },
  );
}

/// _Root : Decides whether to show the login screen or the app.
///
/// A token kept from a previous run is restored first, so that a reload
/// does not ask for a password again. The token is not verified here —
/// doing so would mean a request before anything is on screen — so a stale
/// one is discovered by the first call that uses it, which sends the user
/// back here.
class _Root extends StatefulWidget {
  const _Root({required this.api});

  final FridayApi api;

  @override
  State<_Root> createState() => _RootState();
}

class _RootState extends State<_Root> {
  /// _restoring : True until the stored token has been looked for.
  bool _restoring = true;
  bool _signedIn = false;

  @override
  void initState() {
    super.initState();
    _restore();
  }

  Future<void> _restore() async {
    final found = await widget.api.restore();
    if (!mounted) return;
    setState(() {
      _signedIn = found;
      _restoring = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    if (_restoring) {
      // Briefly, and only on a cold start. A spinner rather than the login
      // screen, which would flash for anyone already signed in.
      return Scaffold(
        backgroundColor: context.colors.background,
        body: const Center(child: FSpinner(size: 22)),
      );
    }

    if (!_signedIn) {
      return LoginScreen(
        api: widget.api,
        onSignedIn: (_) => setState(() => _signedIn = true),
      );
    }

    return HomeScreen(
      api: widget.api,
      onSignedOut: () => setState(() => _signedIn = false),
    );
  }
}

/// HomeScreen : The conversation.
///
/// For now it shows the session this client is in. The sidebar that lets
/// another be chosen replaces the placeholder title bar.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.api, required this.onSignedOut});

  final FridayApi api;
  final VoidCallback onSignedOut;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  late final Conversation _conversation = Conversation(api: widget.api);

  /// _voice : Built only once the device is known to speak or listen, so
  /// that a machine with neither is not offered a microphone that cannot
  /// work.
  VoiceSession? _voice;

  Identity? _identity;
  String? _error;

  @override
  void initState() {
    super.initState();
    _open();
    unawaited(_arrangeVoice());
  }

  @override
  void dispose() {
    _voice?.dispose();
    _conversation.dispose();
    super.dispose();
  }

  /// _arrangeVoice : Sets up speaking and listening.
  ///
  /// Speech starts enabled rather than being probed for first. Probing is
  /// unreliable across platforms — a browser reports no voices until it has
  /// loaded them asynchronously — and the two ways of being wrong are not
  /// equal: starting muted on a device that can speak makes the feature
  /// look broken, while starting enabled on one that cannot just produces
  /// silence, with the answer still on screen to read. The mute control is
  /// there either way.
  Future<void> _arrangeVoice() async {
    if (!mounted) return;
    final session = VoiceSession(
      conversation: _conversation,
      speaker: Speaker(defaultVoice()),
      ears: defaultEars(),
    );
    setState(() => _voice = session);
  }

  /// _open : Finds out who is signed in and where their prompts land, then
  /// loads that session.
  Future<void> _open() async {
    setState(() => _error = null);
    try {
      final identity = await widget.api.me();
      if (!mounted) return;
      setState(() => _identity = identity);
      await _conversation.load(identity.client.activeSessionId);
    } on NotAuthenticated {
      // The stored token was refused, and has already been dropped.
      if (mounted) widget.onSignedOut();
    } on FridayException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  Future<void> _signOut() async {
    await widget.api.logout();
    if (mounted) widget.onSignedOut();
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null) {
      return Scaffold(
        backgroundColor: context.colors.background,
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Padding(
              padding: const EdgeInsets.all(FSpacing.xl),
              child: FBanner(
                message: _error!,
                actionLabel: 'Try again',
                onAction: _open,
              ),
            ),
          ),
        ),
      );
    }

    if (_identity == null) {
      return Scaffold(
        backgroundColor: context.colors.background,
        body: const Center(child: FSpinner(size: 22)),
      );
    }

    return ChatScreen(
      conversation: _conversation,
      voice: _voice,
      leading: const Padding(
        padding: EdgeInsets.only(left: FSpacing.xs),
        child: FWordmark(size: 15),
      ),
      actions: [
        if (_voice != null)
          FButton(
            label: _voice!.awake ? 'Listening for "FRIDAY"' : 'Always awake',
            icon: _voice!.awake
                ? Icons.hearing_rounded
                : Icons.hearing_disabled_rounded,
            variant: _voice!.awake
                ? FButtonVariant.secondary
                : FButtonVariant.ghost,
            compact: true,
            onPressed: () => _voice!.setAwake(!_voice!.awake),
          ),
        if (_voice != null)
          FButton(
            label: _voice!.muted ? 'Muted' : 'Speaking',
            icon: _voice!.muted
                ? Icons.volume_off_outlined
                : Icons.volume_up_outlined,
            variant: FButtonVariant.ghost,
            compact: true,
            onPressed: () => _voice!.toggleMute(),
          ),
        if (!kReleaseMode)
          FButton(
            label: 'Design',
            variant: FButtonVariant.ghost,
            compact: true,
            onPressed: () => Navigator.of(context).pushNamed('/gallery'),
          ),
        FButton(
          label: 'Sign out',
          variant: FButtonVariant.ghost,
          compact: true,
          onPressed: _signOut,
        ),
      ],
    );
  }
}
