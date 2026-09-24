/// The conversation.
library;

import 'package:flutter/material.dart';

import '../design/design.dart';
import '../state/conversation.dart';
import '../state/voice_session.dart';

/// _stickThreshold : How close to the bottom counts as being at the bottom.
///
/// Not zero: a few pixels of rounding should not decide whether the view
/// follows a new answer.
const double _stickThreshold = 80;

/// ChatScreen : Shows a session and lets the user talk in it.
///
/// Scrolling is the part that decides whether this feels right. The view
/// follows new content only while the user is already at the bottom; the
/// moment they scroll up to read something, it stops moving under them and
/// offers to take them back instead.
class ChatScreen extends StatefulWidget {
  const ChatScreen({
    super.key,
    required this.conversation,
    this.voice,
    this.title,
    this.leading,
    this.actions,
  });

  final Conversation conversation;

  /// voice : Speaking and listening. Absent where neither is available, in
  /// which case the microphone is not offered at all rather than offered
  /// and broken.
  final VoiceSession? voice;

  /// title : What this session is called, shown in the bar.
  final String? title;

  /// leading : The menu button, on a narrow window where the sidebar is a
  /// drawer.
  final Widget? leading;

  final List<Widget>? actions;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final ScrollController _scroll = ScrollController();
  final TextEditingController _input = TextEditingController();
  final FocusNode _inputFocus = FocusNode();

  /// _stuck : Whether the view is following new content.
  bool _stuck = true;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    widget.conversation.addListener(_onChanged);
    widget.voice?.addListener(_onChanged);
  }

  @override
  void didUpdateWidget(ChatScreen old) {
    super.didUpdateWidget(old);
    if (old.conversation != widget.conversation) {
      old.conversation.removeListener(_onChanged);
      widget.conversation.addListener(_onChanged);
      _stuck = true;
    }
  }

  @override
  void dispose() {
    widget.conversation.removeListener(_onChanged);
    widget.voice?.removeListener(_onChanged);
    _scroll
      ..removeListener(_onScroll)
      ..dispose();
    _input.dispose();
    _inputFocus.dispose();
    super.dispose();
  }

  /// _onScroll : Notices the user taking over, and giving it back.
  void _onScroll() {
    if (!_scroll.hasClients) return;
    final position = _scroll.position;
    final atBottom =
        position.pixels >= position.maxScrollExtent - _stickThreshold;
    if (atBottom != _stuck) setState(() => _stuck = atBottom);
  }

  /// _onChanged : Follows new content, but only if the user has not scrolled
  /// away to read something.
  void _onChanged() {
    if (!mounted) return;
    setState(() {});
    if (_stuck) _scheduleScroll();
  }

  /// _scheduleScroll : Scrolls after the frame that added the content, since
  /// the new extent is not known until it has been laid out.
  void _scheduleScroll({bool animate = false}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_scroll.hasClients) return;
      final target = _scroll.position.maxScrollExtent;
      if (animate) {
        _scroll.animateTo(target, duration: FMotion.base, curve: FMotion.curve);
      } else {
        // Jump while streaming: an animation restarted on every message
        // never arrives, and the text appears to stutter.
        _scroll.jumpTo(target);
      }
    });
  }

  /// _toggleListening : Opens the microphone, or closes it and sends what
  /// was heard.
  void _toggleListening() {
    final voice = widget.voice;
    if (voice == null) return;
    setState(() => _stuck = true);
    if (voice.listening) {
      voice.stopListening();
    } else {
      voice.startListening();
    }
  }

  /// _send : Sends a prompt and returns the view to the bottom, since the
  /// user has just added to the end of the conversation.
  void _send(String text) {
    setState(() => _stuck = true);
    _scheduleScroll();
    widget.conversation.send(text);
  }

  @override
  Widget build(BuildContext context) {
    final conversation = widget.conversation;
    final voice = widget.voice;
    final running = conversation.running;

    return Scaffold(
      backgroundColor: context.colors.background,
      // The top bar is the screen's own, not a Material AppBar, so nothing
      // insets it for the status bar and the notch — it sat underneath the
      // clock with its buttons unreachable. The bottom is inset too, for the
      // gesture bar that sits under the composer.
      body: SafeArea(
        child: Column(
          children: [
            _TopBar(
              title: widget.title,
              leading: widget.leading,
              actions: widget.actions,
            ),
            const FDivider(),
            Expanded(
              child: Stack(
                children: [
                  _body(conversation),
                  if (!_stuck && conversation.exchanges.isNotEmpty)
                    Positioned(
                      right: FSpacing.lg,
                      bottom: FSpacing.lg,
                      child: _JumpToLatest(
                        onTap: () {
                          setState(() => _stuck = true);
                          _scheduleScroll(animate: true);
                        },
                      ),
                    ),
                ],
              ),
            ),
            if (voice != null) _Preparing(voice: voice),
            if (voice?.problem != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  FSpacing.xl,
                  FSpacing.sm,
                  FSpacing.xl,
                  0,
                ),
                child: Center(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 760),
                    child: FBanner(
                      message: voice!.problem!,
                      tone: FBannerTone.warning,
                    ),
                  ),
                ),
              ),
            _Composer(
              input: _input,
              focus: _inputFocus,
              onSend: _send,
              onStop: running == null ? null : conversation.stop,
              busy: running?.sending ?? false,
              onListen: voice == null ? null : _toggleListening,
              listening: voice?.listening ?? false,
              heard: voice?.heard,
              listeningHint: (voice?.awake ?? false)
                  ? 'Say "FRIDAY" and your question…'
                  : 'Listening…',
            ),
          ],
        ),
      ),
    );
  }

  /// _body : The conversation, or whatever stands in for it.
  Widget _body(Conversation conversation) {
    if (conversation.loading) {
      return const Center(child: FSpinner(size: 22));
    }

    if (conversation.error != null) {
      return Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Padding(
            padding: const EdgeInsets.all(FSpacing.xl),
            child: FBanner(
              message: conversation.error!,
              actionLabel: 'Try again',
              onAction: () => conversation.load(conversation.sessionId),
            ),
          ),
        ),
      );
    }

    if (conversation.exchanges.isEmpty) {
      return const FEmptyState(
        icon: Icons.forum_outlined,
        title: 'Nothing said yet',
        body:
            'Ask FRIDAY something. The answer appears here as it arrives, '
            'a sentence at a time.',
      );
    }

    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.symmetric(
        horizontal: FSpacing.xl,
        vertical: FSpacing.lg,
      ),
      itemCount: conversation.exchanges.length,
      itemBuilder: (context, i) => Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: _ExchangeView(
            exchange: conversation.exchanges[i],
            onStop: conversation.stop,
            onRetry: () => conversation.retry(conversation.exchanges[i]),
          ),
        ),
      ),
    );
  }
}

/// _ExchangeView : One prompt and everything that came back for it.
class _ExchangeView extends StatelessWidget {
  const _ExchangeView({
    required this.exchange,
    required this.onStop,
    required this.onRetry,
  });

  final Exchange exchange;
  final VoidCallback onStop;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        FTurn(
          speaker: FSpeaker.you,
          text: exchange.prompt,
          superseded: exchange.isSuperseded,
        ),

        // Never reached the server, so it can be sent again unchanged.
        if (exchange.rejected)
          Padding(
            padding: const EdgeInsets.only(
              left: FSpacing.lg,
              bottom: FSpacing.sm,
            ),
            child: FBanner(
              message: exchange.failure,
              actionLabel: 'Send again',
              onAction: onRetry,
            ),
          ),

        // What it said while working. Kept after the answer arrives rather
        // than cleared: it is a record of what happened, and removing it
        // would make the conversation jump.
        for (final update in exchange.updates)
          FTurn(
            speaker: FSpeaker.friday,
            text: update,
            transient: true,
            superseded: exchange.isSuperseded,
          ),

        if (exchange.isRunning && exchange.hasSaidNothing)
          FThinkingTurn(onStop: onStop)
        else if (exchange.isRunning)
          Padding(
            padding: const EdgeInsets.only(
              left: FSpacing.lg,
              top: FSpacing.xs,
              bottom: FSpacing.sm,
            ),
            child: Row(
              children: [
                const FThinkingDots(),
                const Spacer(),
                _StopLink(onTap: onStop),
              ],
            ),
          ),

        if (exchange.answer.isNotEmpty)
          FTurn(speaker: FSpeaker.friday, text: exchange.answer),

        if (exchange.failure.isNotEmpty && !exchange.rejected)
          FTurn(speaker: FSpeaker.friday, text: exchange.failure, failed: true),

        if (exchange.isSuperseded)
          Padding(
            padding: const EdgeInsets.only(
              left: FSpacing.lg,
              bottom: FSpacing.sm,
            ),
            child: Text(
              'Stopped — you asked something else.',
              style: context.text.caption,
            ),
          ),

        const SizedBox(height: FSpacing.sm),
      ],
    );
  }
}

/// _StopLink : A quiet stop, beside the progress indicator.
class _StopLink extends StatelessWidget {
  const _StopLink({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Semantics(
    button: true,
    label: 'Stop',
    child: MouseRegion(
      cursor: SystemMouseCursors.click,
      child: GestureDetector(
        onTap: onTap,
        behavior: HitTestBehavior.opaque,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.stop_circle_outlined,
              size: 15,
              color: context.colors.textMuted,
            ),
            const SizedBox(width: FSpacing.xs),
            Text('Stop', style: context.text.caption),
          ],
        ),
      ),
    ),
  );
}

/// _JumpToLatest : Offered when the user has scrolled away from the end and
/// new things are still arriving.
class _JumpToLatest extends StatelessWidget {
  const _JumpToLatest({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Semantics(
      button: true,
      label: 'Jump to the latest',
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: onTap,
          child: Container(
            padding: const EdgeInsets.symmetric(
              horizontal: FSpacing.md,
              vertical: FSpacing.sm,
            ),
            decoration: BoxDecoration(
              color: colors.surfaceRaised,
              border: Border.all(color: colors.border),
              borderRadius: BorderRadius.circular(FRadius.pill),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.arrow_downward_rounded,
                  size: 14,
                  color: colors.textSecondary,
                ),
                const SizedBox(width: FSpacing.xs),
                Text('Latest', style: context.text.caption),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// _Preparing : Says what the wait is for, the first time voice is used.
///
/// Whisper downloads its weights on first use, which takes minutes. Said
/// nothing, an open microphone that hears nothing reads as a broken app.
class _Preparing extends StatelessWidget {
  const _Preparing({required this.voice});

  final VoiceSession voice;

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<double?>(
      valueListenable: voice.preparing,
      builder: (context, progress, _) {
        if (progress == null) return const SizedBox.shrink();

        final percent = (progress * 100).clamp(0, 100).round();
        return Padding(
          padding: const EdgeInsets.fromLTRB(
            FSpacing.xl,
            FSpacing.sm,
            FSpacing.xl,
            0,
          ),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 760),
              child: FBanner(
                message:
                    'Getting speech ready — $percent%. This happens once, '
                    'and needs a good connection.',
              ),
            ),
          ),
        );
      },
    );
  }
}

/// _TopBar : The session's name and whatever belongs beside it.
class _TopBar extends StatelessWidget {
  const _TopBar({this.title, this.leading, this.actions});

  final String? title;
  final Widget? leading;
  final List<Widget>? actions;

  @override
  Widget build(BuildContext context) => Container(
    height: 52,
    padding: const EdgeInsets.symmetric(horizontal: FSpacing.md),
    color: context.colors.surface,
    child: Row(
      children: [
        if (leading != null) ...[leading!, const SizedBox(width: FSpacing.sm)],
        Expanded(
          child: Text(
            title == null || title!.isEmpty ? 'Untitled session' : title!,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.text.subtitle,
          ),
        ),
        ...?actions,
      ],
    ),
  );
}

/// _Composer : The input bar, held to the same measure as the conversation
/// so the two line up.
class _Composer extends StatelessWidget {
  const _Composer({
    required this.input,
    required this.focus,
    required this.onSend,
    required this.onStop,
    required this.busy,
    required this.onListen,
    required this.listening,
    required this.heard,
    required this.listeningHint,
  });

  final TextEditingController input;
  final FocusNode focus;
  final ValueChanged<String> onSend;
  final VoidCallback? onStop;
  final bool busy;
  final VoidCallback? onListen;
  final bool listening;
  final String? heard;
  final String? listeningHint;

  @override
  Widget build(BuildContext context) => Container(
    color: context.colors.background,
    padding: const EdgeInsets.fromLTRB(
      FSpacing.xl,
      FSpacing.sm,
      FSpacing.xl,
      FSpacing.lg,
    ),
    child: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: FComposer(
          controller: input,
          focusNode: focus,
          onSend: onSend,
          onStop: onStop,
          busy: busy,
          onListen: onListen,
          listening: listening,
          heard: heard,
          listeningHint: listeningHint,
        ),
      ),
    ),
  );
}
