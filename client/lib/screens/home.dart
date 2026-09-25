/// The signed-in screen: sessions beside a conversation.
library;

import 'package:flutter/material.dart';

import '../design/design.dart';
import '../state/app_state.dart';
import 'settings.dart';

/// HomeScreen : The sidebar and the conversation.
///
/// Wide enough, and both are on screen at once. Narrower, the sidebar becomes
/// a drawer, because a phone-width column cannot hold a readable conversation
/// and a list of sessions side by side.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.state});

  final AppState state;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final _composer = TextEditingController();
  final _scroll = ScrollController();
  final _scaffold = GlobalKey<ScaffoldState>();

  /// _turnCount : How many turns were on screen when the list last moved, so
  /// that a new one scrolls into view while a growing answer does not fight
  /// the reader for the scroll position.
  int _turnCount = 0;

  @override
  void initState() {
    super.initState();
    widget.state.addListener(_onStateChanged);
  }

  @override
  void dispose() {
    widget.state.removeListener(_onStateChanged);
    _composer.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _onStateChanged() {
    final count = widget.state.turns.length;
    if (count == _turnCount) return;
    _turnCount = count;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: AppMotion.base,
        curve: AppMotion.curve,
      );
    });
  }

  Future<void> _send(String text) async {
    if (text.trim().isEmpty) return;
    _composer.clear();
    await widget.state.send(text);
  }

  /// _rename : Asks for a new name for a session and applies it.
  ///
  /// A dialog rather than editing in place: the tile is also the control that
  /// switches session, and a text field inside it means every attempt to
  /// rename risks navigating away from what you were reading.
  Future<void> _rename(String id, String current) async {
    final field = TextEditingController(text: current);
    final title = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: context.colors.surfaceRaised,
        title: Text('Rename session', style: context.text.subtitle),
        // Sized, because AlertDialog gives its content the whole dialog to
        // fill and a lone text field stretches to the bottom of the screen.
        content: SizedBox(
          width: 360,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              AppTextField(
                controller: field,
                hint: 'Untitled',
                autofocus: true,
                onSubmitted: (value) => Navigator.of(context).pop(value),
              ),
            ],
          ),
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.ghost,
            onPressed: () => Navigator.of(context).pop(),
          ),
          AppButton(
            label: 'Rename',
            onPressed: () => Navigator.of(context).pop(field.text),
          ),
        ],
      ),
    );
    field.dispose();
    if (title != null) await widget.state.rename(id, title);
  }

  void _openSettings() {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => SettingsScreen(state: widget.state),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final compact = context.isCompact;

    return AnimatedBuilder(
      animation: state,
      builder: (context, _) {
        final sidebar = _Sidebar(
          state: state,
          onSettings: _openSettings,
          onRename: _rename,
          onPicked: compact ? () => Navigator.of(context).maybePop() : null,
        );

        return Scaffold(
          key: _scaffold,
          backgroundColor: context.colors.background,
          drawer: compact ? Drawer(child: sidebar) : null,
          body: SafeArea(
            child: Row(
              children: [
                if (!compact) SizedBox(width: 280, child: sidebar),
                if (!compact) const AppDivider(vertical: true),
                Expanded(
                  child: _Conversation(
                    state: state,
                    composer: _composer,
                    scroll: _scroll,
                    onSend: _send,
                    onMenu: compact
                        ? () => _scaffold.currentState?.openDrawer()
                        : null,
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

/// _Sidebar : The sessions, a way to start one, and the way to settings.
class _Sidebar extends StatelessWidget {
  const _Sidebar({
    required this.state,
    required this.onSettings,
    required this.onRename,
    this.onPicked,
  });

  final AppState state;
  final VoidCallback onSettings;

  /// onRename : Called with a session and its current name.
  final void Function(String id, String title) onRename;

  /// onPicked : Called after a session is chosen, so the drawer can close
  /// itself when the sidebar is inside one.
  final VoidCallback? onPicked;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: context.colors.surfaceSunken,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Row(
              children: [
                Expanded(
                  child: Text('Assistant', style: context.text.subtitle),
                ),
                IconButton(
                  onPressed: state.busy ? null : state.refresh,
                  icon: const Icon(Icons.refresh, size: 20),
                  tooltip: 'Refresh',
                  color: context.colors.textSecondary,
                ),
                IconButton(
                  onPressed: onSettings,
                  icon: const Icon(Icons.settings_outlined, size: 20),
                  tooltip: 'Settings',
                  color: context.colors.textSecondary,
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg),
            child: AppButton(
              label: 'New session',
              icon: Icons.add,
              variant: AppButtonVariant.secondary,
              expand: true,
              onPressed: state.busy
                  ? null
                  : () async {
                      await state.newSession();
                      onPicked?.call();
                    },
            ),
          ),
          const SizedBox(height: AppSpacing.md),
          Expanded(
            child: state.sessions.isEmpty
                ? Center(
                    child: Text(
                      'No sessions yet.',
                      style: context.text.caption.copyWith(
                        color: context.colors.textMuted,
                      ),
                    ),
                  )
                : ListView.builder(
                    padding: const EdgeInsets.symmetric(
                      horizontal: AppSpacing.sm,
                    ),
                    itemCount: state.sessions.length,
                    itemBuilder: (context, i) {
                      final s = state.sessions[i];
                      return Padding(
                        padding: const EdgeInsets.only(bottom: AppSpacing.xxs),
                        child: AppSessionTile(
                          title: s.title.isEmpty ? 'Untitled' : s.title,
                          subtitle: _when(s.updatedAt),
                          selected: s.id == state.sessionId,
                          active: s.active,
                          onTap: () async {
                            await state.select(s.id);
                            onPicked?.call();
                          },
                          onRename: () => onRename(s.id, s.title),
                        ),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

/// _Conversation : The turns, and the box to add one.
class _Conversation extends StatelessWidget {
  const _Conversation({
    required this.state,
    required this.composer,
    required this.scroll,
    required this.onSend,
    this.onMenu,
  });

  final AppState state;
  final TextEditingController composer;
  final ScrollController scroll;
  final ValueChanged<String> onSend;
  final VoidCallback? onMenu;

  @override
  Widget build(BuildContext context) {
    final running = state.turns.any((t) => t.isRunning);

    return Column(
      children: [
        if (onMenu != null)
          Padding(
            padding: const EdgeInsets.all(AppSpacing.sm),
            child: Row(
              children: [
                IconButton(
                  onPressed: onMenu,
                  icon: const Icon(Icons.menu),
                  color: context.colors.textSecondary,
                ),
              ],
            ),
          ),
        if (state.error != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              AppSpacing.lg,
              AppSpacing.sm,
              AppSpacing.lg,
              0,
            ),
            child: AppBanner(
              message: state.error!,
              actionLabel: 'Dismiss',
              onAction: state.dismissError,
            ),
          ),
        Expanded(
          child: state.turns.isEmpty
              ? const Center(
                  child: AppEmptyState(
                    icon: Icons.chat_bubble_outline,
                    title: 'Nothing here yet',
                    body: 'Ask something to start this session.',
                  ),
                )
              : ListView.builder(
                  controller: scroll,
                  padding: const EdgeInsets.all(AppSpacing.lg),
                  itemCount: state.turns.length,
                  itemBuilder: (context, i) {
                    final turn = state.turns[i];
                    final failed = turn.error.isNotEmpty;
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        AppTurn(speaker: AppSpeaker.you, text: turn.prompt),
                        const SizedBox(height: AppSpacing.sm),
                        if (turn.isRunning && turn.answer.isEmpty)
                          AppThinkingTurn(onStop: state.cancel)
                        else
                          AppTurn(
                            speaker: AppSpeaker.assistant,
                            text: failed ? turn.error : turn.answer,
                            failed: failed,
                            detail: failed ? turn.detail : null,
                            transient: turn.isRunning,
                          ),
                        const SizedBox(height: AppSpacing.lg),
                      ],
                    );
                  },
                ),
        ),
        Padding(
          padding: const EdgeInsets.all(AppSpacing.lg),
          child: AppComposer(
            controller: composer,
            onSend: onSend,
            onStop: running ? state.cancel : null,
            busy: state.sending,
          ),
        ),
      ],
    );
  }
}

/// _when : A timestamp short enough for a sidebar. Today shows a clock time,
/// anything older shows a date, because the day is what distinguishes them.
String _when(DateTime at) {
  String two(int n) => n.toString().padLeft(2, '0');
  final now = DateTime.now();
  final local = at.toLocal();
  final sameDay = local.year == now.year &&
      local.month == now.month &&
      local.day == now.day;
  return sameDay
      ? '${two(local.hour)}:${two(local.minute)}'
      : '${two(local.day)}/${two(local.month)}';
}
