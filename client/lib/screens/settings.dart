/// Who is signed in, what else holds a token, and where the server is.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../design/design.dart';
import '../state/app_state.dart';

/// SettingsScreen : Account, clients and the server address.
///
/// The server address is shown rather than edited. It is fixed when the app
/// is built — the web bundle is served by the assistant itself, so its own
/// origin is the answer — and a box that looked editable would suggest
/// otherwise.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.state});

  final AppState state;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  @override
  void initState() {
    super.initState();
    widget.state.loadClients();
  }

  Future<void> _confirmRevoke(Client client) async {
    final self = client.current;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: context.colors.surfaceRaised,
        title: Text(
          self ? 'Sign out everywhere?' : 'Revoke this client?',
          style: context.text.subtitle,
        ),
        content: Text(
          self
              ? 'This is the client you are using. Revoking it signs you out '
                    'here as well.'
              : 'Its token stops working immediately. Anything using it will '
                    'have to sign in again.',
          style: context.text.body,
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.ghost,
            onPressed: () => Navigator.of(context).pop(false),
          ),
          AppButton(
            label: 'Revoke',
            variant: AppButtonVariant.danger,
            onPressed: () => Navigator.of(context).pop(true),
          ),
        ],
      ),
    );
    if (ok ?? false) await widget.state.revoke(client.id);
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;

    return AnimatedBuilder(
      animation: state,
      builder: (context, _) {
        final identity = state.identity;
        return Scaffold(
          backgroundColor: context.colors.background,
          appBar: AppBar(
            backgroundColor: context.colors.background,
            surfaceTintColor: Colors.transparent,
            elevation: 0,
            title: Text('Settings', style: context.text.subtitle),
            iconTheme: IconThemeData(color: context.colors.textPrimary),
          ),
          body: ListView(
            padding: const EdgeInsets.all(AppSpacing.lg),
            children: [
              if (state.error != null) ...[
                AppBanner(
                  message: state.error!,
                  actionLabel: 'Dismiss',
                  onAction: state.dismissError,
                ),
                const SizedBox(height: AppSpacing.lg),
              ],
              _Section(
                title: 'Account',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    _Row(
                      label: 'Signed in as',
                      value: identity?.user.username ?? '—',
                    ),
                    _Row(
                      label: 'This client',
                      value: identity == null || identity.client.name.isEmpty
                          ? '—'
                          : identity.client.name,
                    ),
                    const SizedBox(height: AppSpacing.lg),
                    AppButton(
                      label: 'Sign out',
                      variant: AppButtonVariant.secondary,
                      icon: Icons.logout,
                      onPressed: () async {
                        await state.signOut();
                        if (context.mounted) Navigator.of(context).pop();
                      },
                    ),
                  ],
                ),
              ),
              const SizedBox(height: AppSpacing.lg),
              _Section(
                title: 'Server',
                child: _Row(
                  label: 'Address',
                  value: state.api.baseUrl.toString(),
                  monospace: true,
                ),
              ),
              const SizedBox(height: AppSpacing.lg),
              _Section(
                title: 'Clients',
                subtitle:
                    'Everything holding a token for this account, including '
                    'the voice satellite.',
                child: state.clients.isEmpty
                    ? Text(
                        'Nothing to show.',
                        style: context.text.caption.copyWith(
                          color: context.colors.textMuted,
                        ),
                      )
                    : Column(
                        children: [
                          for (final c in state.clients)
                            _ClientRow(
                              client: c,
                              onRevoke: c.revoked
                                  ? null
                                  : () => _confirmRevoke(c),
                              onChannel: c.revoked
                                  ? null
                                  : (channel) => state.setClientChannel(
                                      c.id,
                                      channel,
                                    ),
                            ),
                        ],
                      ),
              ),
            ],
          ),
        );
      },
    );
  }
}

/// _Section : A titled card, so the page reads as a few groups rather than
/// one long list of fields.
class _Section extends StatelessWidget {
  const _Section({required this.title, required this.child, this.subtitle});

  final String title;
  final String? subtitle;
  final Widget child;

  @override
  Widget build(BuildContext context) => AppSurface(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(title, style: context.text.label),
        if (subtitle != null) ...[
          const SizedBox(height: AppSpacing.xxs),
          Text(
            subtitle!,
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          ),
        ],
        const SizedBox(height: AppSpacing.md),
        child,
      ],
    ),
  );
}

/// _Row : A label with its value, wrapping rather than clipping.
class _Row extends StatelessWidget {
  const _Row({
    required this.label,
    required this.value,
    this.monospace = false,
  });

  final String label;
  final String value;
  final bool monospace;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 120,
          child: Text(
            label,
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          ),
        ),
        Expanded(
          child: SelectableText(
            value,
            style: monospace ? context.text.mono : context.text.body,
          ),
        ),
      ],
    ),
  );
}

/// _ClientRow : One client, with the way to take its token away.
class _ClientRow extends StatelessWidget {
  const _ClientRow({required this.client, this.onRevoke, this.onChannel});

  final Client client;
  final VoidCallback? onRevoke;

  /// onChannel : Called with "voice" or "direct".
  ///
  /// Offered because a client cannot always declare itself: Home Assistant is
  /// handed a token through a screen with no field for it, so its client
  /// registers as direct and has to be corrected here.
  final ValueChanged<String>? onChannel;

  @override
  Widget build(BuildContext context) {
    final name = client.name.isEmpty ? client.id : client.name;
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Flexible(child: Text(name, style: context.text.body)),
                    if (client.current) ...[
                      const SizedBox(width: AppSpacing.sm),
                      Text(
                        'this one',
                        style: context.text.caption.copyWith(
                          color: context.colors.accent,
                        ),
                      ),
                    ],
                    if (client.revoked) ...[
                      const SizedBox(width: AppSpacing.sm),
                      Text(
                        'revoked',
                        style: context.text.caption.copyWith(
                          color: context.colors.textMuted,
                        ),
                      ),
                    ],
                  ],
                ),
                Text(
                  client.id,
                  style: context.text.caption.copyWith(
                    color: context.colors.textMuted,
                  ),
                ),
                if (onChannel != null) ...[
                  const SizedBox(height: AppSpacing.xs),
                  _ChannelChoice(
                    channel: client.channel,
                    onChanged: onChannel!,
                  ),
                ],
              ],
            ),
          ),
          if (onRevoke != null)
            AppButton(
              label: 'Revoke',
              variant: AppButtonVariant.ghost,
              compact: true,
              onPressed: onRevoke,
            ),
        ],
      ),
    );
  }
}

/// _ChannelChoice : Whether a client's prompts count as spoken or typed.
///
/// Shown as two words rather than a switch, because the two are not on and
/// off: they say what the thing holding the token is, and which one is
/// selected has to be readable at a glance in a list.
class _ChannelChoice extends StatelessWidget {
  const _ChannelChoice({required this.channel, required this.onChanged});

  final String channel;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      for (final option in const ['voice', 'direct'])
        Padding(
          padding: const EdgeInsets.only(right: AppSpacing.sm),
          child: InkWell(
            onTap: channel == option ? null : () => onChanged(option),
            borderRadius: BorderRadius.circular(AppRadius.xs),
            child: Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.sm,
                vertical: AppSpacing.xxs,
              ),
              child: Text(
                option,
                style: context.text.caption.copyWith(
                  color: channel == option
                      ? context.colors.accent
                      : context.colors.textMuted,
                ),
              ),
            ),
          ),
        ),
    ],
  );
}
