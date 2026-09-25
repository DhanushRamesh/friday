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
  final _client = TextEditingController();

  @override
  void dispose() {
    _username.dispose();
    _password.dispose();
    _client.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (widget.state.busy) return;
    await widget.state.signIn(
      username: _username.text.trim(),
      password: _password.text,
      // The remembered name wins when there is one, because the field is not
      // shown in that case and would be empty.
      clientName: widget.state.clientName ?? _client.text.trim(),
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
                  textInputAction: TextInputAction.next,
                  autofillHints: const [AutofillHints.password],
                ),
                const SizedBox(height: AppSpacing.lg),
                // Asked once. Signing out takes the token away; it does not
                // make this a different browser, and asking again would
                // invite a second name for the same thing.
                if (state.clientName == null) ...[
                  AppTextField(
                    controller: _client,
                    label: 'Name this browser',
                    hint: 'chrome-dhanush',
                    enabled: !state.busy,
                    textInputAction: TextInputAction.go,
                    onSubmitted: (_) => _submit(),
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  Text(
                    'Signing in registers this browser separately, so what '
                    'you type here is not mistaken for what you say aloud.',
                    style: context.text.caption.copyWith(
                      color: context.colors.textMuted,
                    ),
                  ),
                ] else
                  _KnownBrowser(
                    name: state.clientName!,
                    onChange: state.busy ? null : state.forgetClientName,
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

/// _KnownBrowser : Says which browser this is, without asking again.
class _KnownBrowser extends StatelessWidget {
  const _KnownBrowser({required this.name, this.onChange});

  final String name;
  final VoidCallback? onChange;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Icon(Icons.computer_outlined, size: 14, color: context.colors.textMuted),
      const SizedBox(width: AppSpacing.sm),
      Expanded(
        child: Text(
          'Signing in as $name',
          style: context.text.caption.copyWith(
            color: context.colors.textSecondary,
          ),
        ),
      ),
      if (onChange != null)
        InkWell(
          onTap: onChange,
          borderRadius: BorderRadius.circular(AppRadius.xs),
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.xxs),
            child: Text(
              'Change',
              style: context.text.caption.copyWith(
                color: context.colors.accent,
              ),
            ),
          ),
        ),
    ],
  );
}
