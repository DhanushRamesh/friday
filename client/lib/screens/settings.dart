/// Who is signed in, what else holds a token, and where the server is.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../design/design.dart';
import '../state/app_state.dart';

/// SettingsModule : One page of settings, named down the side.
enum SettingsModule {
  account('Account', Icons.person_outline),
  clients('Clients', Icons.devices_other_outlined),
  server('Server', Icons.dns_outlined);

  const SettingsModule(this.title, this.icon);

  final String title;
  final IconData icon;
}

/// SettingsScreen : The settings, one module at a time.
///
/// Separate pages rather than one scrolling list: clients is the only part
/// that is a list of things to act on, and putting it under the account
/// fields meant scrolling past them to reach it. Down a side it can also
/// grow — sessions and voice belong here eventually — without the page
/// getting longer.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.state});

  final AppState state;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  SettingsModule _module = SettingsModule.account;

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
    final compact = context.isCompact;

    return AnimatedBuilder(
      animation: state,
      builder: (context, _) {
        return Scaffold(
          backgroundColor: context.colors.background,
          appBar: AppBar(
            backgroundColor: context.colors.background,
            surfaceTintColor: Colors.transparent,
            elevation: 0,
            title: Text(
              compact ? _module.title : 'Settings',
              style: context.text.subtitle,
            ),
            iconTheme: IconThemeData(color: context.colors.textPrimary),
          ),
          body: SafeArea(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (!compact) ...[
                  SizedBox(
                    width: 200,
                    child: _ModuleList(
                      selected: _module,
                      onPick: (m) => setState(() => _module = m),
                    ),
                  ),
                  const AppDivider(vertical: true),
                ],
                Expanded(
                  child: ListView(
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
                      // On a narrow screen the modules are a row of chips
                      // above the page: a column beside it would leave
                      // neither enough width to read.
                      if (compact) ...[
                        _ModuleChips(
                          selected: _module,
                          onPick: (m) => setState(() => _module = m),
                        ),
                        const SizedBox(height: AppSpacing.lg),
                      ],
                      switch (_module) {
                        SettingsModule.account => _AccountModule(state: state),
                        SettingsModule.clients => _ClientsModule(
                          state: state,
                          onRevoke: _confirmRevoke,
                        ),
                        SettingsModule.server => _ServerModule(state: state),
                      },
                    ],
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

/// _ModuleList : The modules down the side.
class _ModuleList extends StatelessWidget {
  const _ModuleList({required this.selected, required this.onPick});

  final SettingsModule selected;
  final ValueChanged<SettingsModule> onPick;

  @override
  Widget build(BuildContext context) => ColoredBox(
    color: context.colors.surfaceSunken,
    child: ListView(
      padding: const EdgeInsets.all(AppSpacing.sm),
      children: [
        for (final m in SettingsModule.values)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.xxs),
            child: _ModuleTile(
              module: m,
              selected: m == selected,
              onTap: () => onPick(m),
            ),
          ),
      ],
    ),
  );
}

/// _ModuleTile : One name down the side.
class _ModuleTile extends StatelessWidget {
  const _ModuleTile({
    required this.module,
    required this.selected,
    required this.onTap,
  });

  final SettingsModule module;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: AppSpacing.sm + 2,
        ),
        decoration: BoxDecoration(
          color: selected ? colors.accentSoft : Colors.transparent,
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Row(
          children: [
            Icon(
              module.icon,
              size: 16,
              color: selected ? colors.accent : colors.textMuted,
            ),
            const SizedBox(width: AppSpacing.sm),
            Text(
              module.title,
              style: context.text.body.copyWith(
                color: selected ? colors.textPrimary : colors.textSecondary,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// _ModuleChips : The modules as a row, for a screen too narrow for a column.
class _ModuleChips extends StatelessWidget {
  const _ModuleChips({required this.selected, required this.onPick});

  final SettingsModule selected;
  final ValueChanged<SettingsModule> onPick;

  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    children: [
      for (final m in SettingsModule.values)
        InkWell(
          onTap: () => onPick(m),
          borderRadius: BorderRadius.circular(AppRadius.pill),
          child: Container(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.md,
              vertical: AppSpacing.xs,
            ),
            decoration: BoxDecoration(
              color: m == selected
                  ? context.colors.accentSoft
                  : context.colors.surfaceRaised,
              borderRadius: BorderRadius.circular(AppRadius.pill),
            ),
            child: Text(
              m.title,
              style: context.text.caption.copyWith(
                color: m == selected
                    ? context.colors.accent
                    : context.colors.textSecondary,
              ),
            ),
          ),
        ),
    ],
  );
}

/// _AccountModule : Who is signed in, and the way out.
class _AccountModule extends StatelessWidget {
  const _AccountModule({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final identity = state.identity;
    return _Section(
      title: 'Account',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _Row(label: 'Signed in as', value: identity?.user.username ?? '—'),
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
    );
  }
}

/// _ServerModule : Where the assistant is.
class _ServerModule extends StatelessWidget {
  const _ServerModule({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) => _Section(
    title: 'Server',
    subtitle:
        'Fixed when the app is built. The web bundle is meant to be served '
        'by the assistant itself, so its own origin is the answer.',
    child: _Row(
      label: 'Address',
      value: state.api.baseUrl.toString(),
      monospace: true,
    ),
  );
}

/// _ClientsModule : Everything holding a token, and what each one is.
class _ClientsModule extends StatelessWidget {
  const _ClientsModule({required this.state, required this.onRevoke});

  final AppState state;
  final ValueChanged<Client> onRevoke;

  @override
  Widget build(BuildContext context) => _Section(
    title: state.showRevoked ? 'Revoked clients' : 'Clients',
    subtitle: state.showRevoked
        ? 'Kept so a revocation is visible rather than silently absent. None '
              'of these can sign in, and none can be brought back.'
        : 'Everything holding a token for this account, including the voice '
              'satellite. A client says what it is when it registers; Home '
              'Assistant cannot, so its channel is set here.',
    action: InkWell(
      onTap: () => state.setShowRevoked(!state.showRevoked),
      borderRadius: BorderRadius.circular(AppRadius.xs),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.xxs),
        child: Text(
          state.showRevoked ? 'Show active' : 'Show revoked',
          style: context.text.caption.copyWith(color: context.colors.accent),
        ),
      ),
    ),
    child: state.clients.isEmpty
        ? Text(
            state.showRevoked ? 'Nothing revoked.' : 'Nothing to show.',
            style: context.text.caption.copyWith(
              color: context.colors.textMuted,
            ),
          )
        : Column(
            children: [
              for (final c in state.clients)
                _ClientRow(
                  client: c,
                  onRevoke: c.revoked ? null : () => onRevoke(c),
                  onChannel: c.revoked
                      ? null
                      : (channel) => state.setClientChannel(c.id, channel),
                ),
            ],
          ),
  );
}

/// _Section : A titled card, so the page reads as a few groups rather than
/// one long list of fields.
class _Section extends StatelessWidget {
  const _Section({
    required this.title,
    required this.child,
    this.subtitle,
    this.action,
  });

  final String title;
  final String? subtitle;
  final Widget child;

  /// action : Something to do with the whole section, shown beside its title.
  final Widget? action;

  @override
  Widget build(BuildContext context) => AppSurface(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(child: Text(title, style: context.text.label)),
            ?action,
          ],
        ),
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
