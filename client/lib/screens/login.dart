/// Signing in.
library;

import 'package:flutter/material.dart';

import '../design/design.dart';
import '../friday/friday.dart';

/// LoginScreen : Asks for a username and password and hands back the
/// identity FRIDAY issued.
///
/// There is no signup: FRIDAY sits on a public address with the owner's
/// provider credentials behind it, so accounts are created on the server
/// with `createuser` rather than by anyone who finds the page.
class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key, required this.api, required this.onSignedIn});

  final FridayApi api;

  /// onSignedIn : Called once a token has been issued and kept.
  final ValueChanged<LoginResult> onSignedIn;

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _username = TextEditingController();
  final _password = TextEditingController();

  bool _busy = false;

  /// _fieldError : Shown against the password, for a refusal that names
  /// neither field because the server will not say which was wrong.
  String? _fieldError;

  /// _banner : For a failure that is not about what was typed, such as the
  /// server being unreachable. Kept apart so that "wrong password" and "no
  /// network" do not look like the same problem.
  String? _banner;

  @override
  void dispose() {
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  /// _submit : Attempts the login, turning each failure into the message
  /// that belongs where it can be acted on.
  Future<void> _submit() async {
    if (_busy) return;

    final username = _username.text.trim();
    final password = _password.text;
    if (username.isEmpty || password.isEmpty) {
      setState(() => _fieldError = 'Enter your username and password.');
      return;
    }

    setState(() {
      _busy = true;
      _fieldError = null;
      _banner = null;
    });

    try {
      final result = await widget.api.login(
        username: username,
        password: password,
        clientName: _clientName(),
      );
      if (!mounted) return;
      widget.onSignedIn(result);
    } on NotAuthenticated catch (e) {
      // The server refuses a wrong password and an unknown username
      // identically, on purpose, so this says no more than it does.
      if (mounted) setState(() => _fieldError = e.message);
    } on Refused catch (e) {
      if (mounted) setState(() => _fieldError = e.message);
    } on FridayException catch (e) {
      if (mounted) setState(() => _banner = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// _clientName : What this client is called in a listing.
  ///
  /// A token belongs to a client, and the listing is how a lost one is
  /// found and revoked, so the name has to mean something to the user.
  String _clientName() {
    final width = MediaQuery.sizeOf(context).width;
    return width <= FBreakpoints.compact ? 'Phone browser' : 'Web browser';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: context.colors.background,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(FSpacing.xl),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 380),
              child: AutofillGroup(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const Center(child: FWordmark(size: 26)),
                    const SizedBox(height: FSpacing.sm),
                    Center(
                      child: Text(
                        'Sign in to continue',
                        style: context.text.caption,
                      ),
                    ),
                    const SizedBox(height: FSpacing.xxl),

                    if (_banner != null) ...[
                      FBanner(
                        message: _banner!,
                        actionLabel: 'Try again',
                        onAction: _busy ? null : _submit,
                      ),
                      const SizedBox(height: FSpacing.lg),
                    ],

                    FSurface(
                      padding: const EdgeInsets.all(FSpacing.xl),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          FTextField(
                            controller: _username,
                            label: 'Username',
                            icon: Icons.person_outline,
                            autofocus: true,
                            enabled: !_busy,
                            textInputAction: TextInputAction.next,
                            autofillHints: const [AutofillHints.username],
                            onChanged: (_) => _clearError(),
                          ),
                          const SizedBox(height: FSpacing.lg),
                          FTextField(
                            controller: _password,
                            label: 'Password',
                            icon: Icons.lock_outline,
                            obscure: true,
                            enabled: !_busy,
                            error: _fieldError,
                            textInputAction: TextInputAction.go,
                            autofillHints: const [AutofillHints.password],
                            onChanged: (_) => _clearError(),
                            onSubmitted: (_) => _submit(),
                          ),
                          const SizedBox(height: FSpacing.xl),
                          FButton(
                            label: 'Sign in',
                            busy: _busy,
                            expand: true,
                            onPressed: _submit,
                          ),
                        ],
                      ),
                    ),

                    const SizedBox(height: FSpacing.lg),
                    Text(
                      'Accounts are created on the server. There is no '
                      'signup here, so nobody who finds this address can '
                      'make one.',
                      textAlign: TextAlign.center,
                      style: context.text.caption,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// _clearError : Removes the refusal as soon as the user changes what
  /// they typed, so it does not sit there contradicting the new value.
  void _clearError() {
    if (_fieldError != null) setState(() => _fieldError = null);
  }
}
