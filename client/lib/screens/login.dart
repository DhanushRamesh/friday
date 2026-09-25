/// Signing in.
library;

import 'package:flutter/material.dart';

import '../design/design.dart';
import '../state/app_state.dart';

/// LoginScreen : Username and password, and nothing else.
///
/// There is no way to create an account here. Users are made on the server
/// with `make createuser`, which is the only place a password is set, so
/// offering a sign-up would be offering something that cannot work.
class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key, required this.state});

  final AppState state;

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _username = TextEditingController();
  final _password = TextEditingController();

  @override
  void dispose() {
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (widget.state.busy) return;
    await widget.state.signIn(
      username: _username.text.trim(),
      password: _password.text,
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    return Scaffold(
      backgroundColor: context.colors.background,
      body: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(AppSpacing.xl),
          child: AppSurface(
            width: 380,
            padding: const EdgeInsets.all(AppSpacing.xl),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('Assistant', style: context.text.title),
                const SizedBox(height: AppSpacing.xs),
                Text(
                  'Sign in to continue.',
                  style: context.text.caption.copyWith(
                    color: context.colors.textSecondary,
                  ),
                ),
                const SizedBox(height: AppSpacing.xl),
                if (state.error != null) ...[
                  AppBanner(message: state.error!),
                  const SizedBox(height: AppSpacing.lg),
                ],
                AppTextField(
                  controller: _username,
                  label: 'Username',
                  autofocus: true,
                  enabled: !state.busy,
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.username],
                ),
                const SizedBox(height: AppSpacing.lg),
                AppTextField(
                  controller: _password,
                  label: 'Password',
                  obscure: true,
                  enabled: !state.busy,
                  textInputAction: TextInputAction.go,
                  autofillHints: const [AutofillHints.password],
                  onSubmitted: (_) => _submit(),
                ),
                const SizedBox(height: AppSpacing.xl),
                AppButton(
                  label: 'Sign in',
                  onPressed: state.busy ? null : _submit,
                  busy: state.busy,
                  expand: true,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
