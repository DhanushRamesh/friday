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
/// grow — conversations and voice belong here eventually — without the page
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
          if (state.personas.options.isNotEmpty) ...[
            const SizedBox(height: AppSpacing.lg),
            _Field(
              label: 'Manner',
              child: _Select(
                value: state.personas.currentName,
                options: [
                  for (final p in state.personas.options)
                    _Option(
                      label: '${p.name}  ·  ${p.summary}',
                      selected: p.id == state.personas.current,
                      onTap: () => state.setPersona(p.id),
                    ),
                ],
              ),
            ),
            const SizedBox(height: AppSpacing.xs),
            Text(
              'How the assistant speaks, everywhere. It takes effect on the '
              'next thing you ask, and is remembered across restarts.',
              style: context.text.caption.copyWith(
                color: context.colors.textMuted,
              ),
            ),
          ],
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
        : 'Everything holding a token for this account. How a client\'s '
              'prompts arrive decides what the assistant may do about them, '
              'and each one can be answered by a different model \u2014 a '
              'spoken answer has to arrive quickly, while an app can wait for '
              'a better one.',
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
                  catalogue: state.catalogue,
                  onRevoke: c.revoked ? null : () => onRevoke(c),
                  onChannel: c.revoked
                      ? null
                      : (channel) => state.setClientChannel(c.id, channel),
                  onModel: c.revoked
                      ? null
                      : (model) => state.setClientModel(
                          c.id,
                          model?.vendor ?? '',
                          model?.id ?? '',
                        ),
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
  const _ClientRow({
    required this.client,
    this.catalogue = const ModelCatalogue(),
    this.onRevoke,
    this.onChannel,
    this.onModel,
  });

  final Client client;

  /// catalogue : What this client may be answered by, and what answers it
  /// when it has chosen nothing.
  final ModelCatalogue catalogue;

  final VoidCallback? onRevoke;

  /// onChannel : Called with "voice" or "direct".
  ///
  /// Offered because a client cannot always declare itself: Home Assistant is
  /// handed a token through a screen with no field for it, so its client
  /// registers as direct and has to be corrected here.
  final ValueChanged<String>? onChannel;

  /// onModel : Called with the model to answer this client, or null to put it
  /// back on the server's.
  final ValueChanged<LlmModel?>? onModel;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final name = client.name.isEmpty ? client.id : client.name;

    // A client cannot raise its own channel, so the field is shown as a fact
    // rather than as a control that refuses.
    final ownChannel = client.current;

    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
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
                          _Tag(
                            'you are signed in here',
                            color: colors.accent,
                          ),
                        ],
                        if (client.revoked) ...[
                          const SizedBox(width: AppSpacing.sm),
                          _Tag('revoked', color: colors.textMuted),
                        ],
                      ],
                    ),
                    Text(
                      client.id,
                      style: context.text.caption.copyWith(
                        color: colors.textMuted,
                      ),
                    ),
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
          if (onChannel != null || onModel != null) ...[
            const SizedBox(height: AppSpacing.sm),
            Wrap(
              spacing: AppSpacing.xl,
              runSpacing: AppSpacing.sm,
              children: [
                if (onChannel != null)
                  _Field(
                    label: 'Prompts arrive',
                    child: _Select(
                      value: _channelLabel(client.channel),
                      hint: ownChannel
                          ? 'A client cannot change its own. Use another one.'
                          : null,
                      options: [
                        for (final entry in _channels.entries)
                          _Option(
                            label: entry.value,
                            selected: client.channel == entry.key,
                            onTap: ownChannel
                                ? null
                                : () => onChannel!(entry.key),
                          ),
                      ],
                    ),
                  ),
                if (onModel != null && catalogue.models.isNotEmpty)
                  _Field(
                    label: 'Answered by',
                    child: _Select(
                      value: _modelLabel(),
                      options: [
                        _Option(
                          label: catalogue.defaultName.isEmpty
                              ? 'Server default'
                              : 'Server default \u00b7 ${catalogue.defaultName}',
                          selected: client.model.isEmpty,
                          onTap: () => onModel!(null),
                        ),
                        for (final m in catalogue.models)
                          _Option(
                            label: '${m.name}  \u00b7  '
                                '${_thousands(m.contextTokens)} tokens',
                            selected: m.id == client.model,
                            onTap: () => onModel!(m),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  /// _modelLabel : What the model field reads as when closed.
  ///
  /// A client that has chosen nothing shows the model that will answer it
  /// anyway, since "server default" on its own tells a person only that they
  /// have not been told.
  String _modelLabel() {
    for (final m in catalogue.models) {
      if (m.id == client.model) return m.name;
    }
    return catalogue.defaultName.isEmpty
        ? 'Server default'
        : '${catalogue.defaultName}  (server default)';
  }
}

/// _channels : How each channel is named to a person, in the order they are
/// offered.
const Map<String, String> _channels = {
  'voice': 'Spoken, through the satellite',
  'direct': 'Typed, through an app or the API',
};

/// _channelLabel : The short form, for the closed field.
String _channelLabel(String channel) =>
    switch (channel) { 'voice' => 'Spoken', 'direct' => 'Typed', _ => channel };

/// _Tag : A short word beside a name, such as which client you are using.
class _Tag extends StatelessWidget {
  const _Tag(this.text, {required this.color});

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) =>
      Text(text, style: context.text.caption.copyWith(color: color));
}

/// _Field : A labelled control, so a value is never a bare word whose meaning
/// has to be guessed from its position.
class _Field extends StatelessWidget {
  const _Field({required this.label, required this.child});

  final String label;
  final Widget child;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        label,
        style: context.text.caption.copyWith(color: context.colors.textMuted),
      ),
      const SizedBox(height: AppSpacing.xxs),
      child,
    ],
  );
}

/// _Option : One entry in a _Select. A null onTap is an entry that cannot be
/// chosen, which is how the already-selected one and a forbidden one are both
/// shown.
class _Option {
  const _Option({required this.label, required this.selected, this.onTap});

  final String label;
  final bool selected;
  final VoidCallback? onTap;
}

/// _Select : A value with a menu behind it.
///
/// The same control for the channel and for the model, because they are the
/// same kind of thing: one of a short list, and the list is worth reading
/// before choosing. Two words side by side said neither what they were nor
/// that they could be changed.
class _Select extends StatelessWidget {
  const _Select({required this.value, required this.options, this.hint});

  final String value;
  final List<_Option> options;

  /// hint : Why the field cannot be changed, when it cannot.
  final String? hint;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final locked = options.every((o) => o.onTap == null);

    return PopupMenuButton<int>(
      tooltip: hint ?? '',
      enabled: !locked,
      position: PopupMenuPosition.under,
      color: colors.surface,
      onSelected: (i) => options[i].onTap?.call(),
      itemBuilder: (context) => [
        for (var i = 0; i < options.length; i++)
          PopupMenuItem(
            value: i,
            enabled: options[i].onTap != null,
            child: Text(
              options[i].label,
              style: context.text.caption.copyWith(
                color: options[i].selected
                    ? colors.accent
                    : colors.textSecondary,
              ),
            ),
          ),
        if (hint != null)
          PopupMenuItem(
            enabled: false,
            child: Text(
              hint!,
              style: context.text.caption.copyWith(color: colors.textMuted),
            ),
          ),
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.sm,
          vertical: AppSpacing.xxs + 1,
        ),
        decoration: BoxDecoration(
          border: Border.all(color: colors.border),
          borderRadius: BorderRadius.circular(AppRadius.xs),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              value,
              style: context.text.caption.copyWith(
                color: locked ? colors.textMuted : colors.textPrimary,
              ),
            ),
            const SizedBox(width: AppSpacing.xxs),
            Icon(
              locked ? Icons.lock_outline : Icons.expand_more,
              size: 14,
              color: colors.textMuted,
            ),
          ],
        ),
      ),
    );
  }
}


/// _thousands : A count with separators, so 200000 reads as a size rather
/// than a string of noughts.
String _thousands(int n) {
  final digits = n.toString();
  final out = StringBuffer();
  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) out.write(',');
    out.write(digits[i]);
  }
  return out.toString();
}
