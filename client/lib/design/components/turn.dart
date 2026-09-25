import 'package:flutter/material.dart';

import '../tokens.dart';
import 'spinner.dart';

/// AppSpeaker : Who said something.
enum AppSpeaker { you, assistant }

/// AppTurn : One thing said in a conversation.
///
/// Not a bubble on alternating sides: an answer can be long, and a bubble
/// constrained to half the width makes it hard to read. The speaker is shown
/// by a label and by the accent bar down the side, which leaves the full
/// measure for the text.
class AppTurn extends StatelessWidget {
  const AppTurn({
    super.key,
    required this.speaker,
    required this.text,
    this.transient = false,
    this.failed = false,
    this.detail,
    this.superseded = false,
    this.stopped = false,
    this.timestamp,
    this.trailing,
  });

  final AppSpeaker speaker;
  final String text;

  /// transient : Whether this is progress rather than the answer. Shown
  /// dimmer, because it will be overtaken.
  final bool transient;

  /// failed : Whether this is the reason a chat failed.
  final bool failed;

  /// detail : The exact error behind a failure, revealed on request.
  ///
  /// Hidden by default rather than shown small, because the sentence above it
  /// is the whole answer for almost everybody, and a stack of raw service
  /// errors down the conversation makes the readable part hard to find.
  final String? detail;

  /// superseded : Whether a later prompt cancelled this one. Shown faded
  /// rather than removed, so the conversation still reads in order and the
  /// user can see what was dropped.
  final bool superseded;

  /// stopped : Whether the person stopped the turn before it finished.
  ///
  /// Marked rather than left blank. Whatever was said before the stop is
  /// still shown, because a turn that got half an answer out said something,
  /// and a turn that got none still happened.
  final bool stopped;

  final String? timestamp;

  /// trailing : An action for this turn, such as stop while it runs.
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final isYou = speaker == AppSpeaker.you;

    final accent = failed
        ? colors.danger
        : isYou
        ? colors.borderStrong
        : colors.accent;

    var style = context.text.body;
    if (failed) {
      style = style.copyWith(color: colors.danger);
    } else if (transient) {
      style = style.copyWith(color: colors.textSecondary);
    }
    if (superseded) {
      style = style.copyWith(
        color: colors.textMuted,
        decoration: TextDecoration.lineThrough,
        decorationColor: colors.textMuted,
      );
    }

    return Opacity(
      opacity: superseded ? 0.65 : 1,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // The bar, not an avatar: it marks the speaker without taking a
            // square of space beside every line.
            Container(
              width: 2,
              constraints: const BoxConstraints(minHeight: 18),
              margin: const EdgeInsets.only(top: 3, right: AppSpacing.md),
              decoration: BoxDecoration(
                color: accent,
                borderRadius: BorderRadius.circular(AppRadius.pill),
              ),
            ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Text(
                        isYou ? 'You' : 'Assistant',
                        style: context.text.label.copyWith(
                          color: isYou ? colors.textMuted : colors.accent,
                        ),
                      ),
                      if (timestamp != null) ...[
                        const SizedBox(width: AppSpacing.sm),
                        Text(timestamp!, style: context.text.caption),
                      ],
                      if (trailing != null) ...[const Spacer(), trailing!],
                    ],
                  ),
                  const SizedBox(height: AppSpacing.xs + 2),
                  // Selectable, because the usual thing to do with an answer
                  // is copy part of it somewhere else.
                  if (text.isNotEmpty) SelectableText(text, style: style),
                  if (stopped) _StoppedNote(spaced: text.isNotEmpty),
                  if ((detail ?? '').isNotEmpty) _MoreInfo(detail: detail!),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// _StoppedNote : Says that the person stopped the turn.
class _StoppedNote extends StatelessWidget {
  const _StoppedNote({required this.spaced});

  /// spaced : Whether something was said before the stop, which the note then
  /// has to be separated from.
  final bool spaced;

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.only(top: spaced ? AppSpacing.sm : 0),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          Icons.stop_circle_outlined,
          size: 13,
          color: context.colors.textMuted,
        ),
        const SizedBox(width: AppSpacing.xs + 2),
        Text(
          spaced ? 'Stopped here.' : 'Stopped before it answered.',
          style: context.text.caption.copyWith(
            color: context.colors.textMuted,
            fontStyle: FontStyle.italic,
          ),
        ),
      ],
    ),
  );
}

/// AppThinkingTurn : The placeholder while the assistant has been asked something and
/// has not said anything yet.
class AppThinkingTurn extends StatelessWidget {
  const AppThinkingTurn({super.key, this.onStop});

  /// onStop : Cancels the chat. Present from the first moment, because the
  /// answer may be long and the user may already have changed their mind.
  final VoidCallback? onStop;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 2,
            height: 18,
            margin: const EdgeInsets.only(top: 3, right: AppSpacing.md),
            decoration: BoxDecoration(
              color: colors.accent,
              borderRadius: BorderRadius.circular(AppRadius.pill),
            ),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      'Assistant',
                      style: context.text.label.copyWith(color: colors.accent),
                    ),
                    if (onStop != null) ...[
                      const Spacer(),
                      _StopButton(onTap: onStop!),
                    ],
                  ],
                ),
                const SizedBox(height: AppSpacing.sm),
                const AppThinkingDots(),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// _StopButton : The quiet stop control shown while a chat runs.
class _StopButton extends StatelessWidget {
  const _StopButton({required this.onTap});

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
            const SizedBox(width: AppSpacing.xs),
            Text('Stop', style: context.text.caption),
          ],
        ),
      ),
    ),
  );
}

/// _MoreInfo : The exact error, behind a line you have to click.
///
/// The point of hiding it is that two people want different things from the
/// same failure: one wants to know whether to try again, the other wants the
/// words the service used. Showing both at once serves neither.
class _MoreInfo extends StatefulWidget {
  const _MoreInfo({required this.detail});

  final String detail;

  @override
  State<_MoreInfo> createState() => _MoreInfoState();
}

class _MoreInfoState extends State<_MoreInfo> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SizedBox(height: AppSpacing.xs),
        InkWell(
          onTap: () => setState(() => _open = !_open),
          borderRadius: BorderRadius.circular(AppRadius.xs),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: AppSpacing.xxs),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  _open ? Icons.expand_less : Icons.expand_more,
                  size: 14,
                  color: colors.textMuted,
                ),
                const SizedBox(width: AppSpacing.xxs),
                Text(
                  _open ? 'Hide details' : 'More info',
                  style: context.text.caption.copyWith(color: colors.textMuted),
                ),
              ],
            ),
          ),
        ),
        if (_open)
          Container(
            width: double.infinity,
            margin: const EdgeInsets.only(top: AppSpacing.xs),
            padding: const EdgeInsets.all(AppSpacing.sm),
            decoration: BoxDecoration(
              color: colors.surfaceSunken,
              borderRadius: BorderRadius.circular(AppRadius.sm),
              border: Border.all(color: colors.border),
            ),
            // Monospace and selectable: this exists to be read closely and
            // pasted into a bug report.
            child: SelectableText(
              widget.detail,
              style: context.text.mono.copyWith(color: colors.textSecondary),
            ),
          ),
      ],
    );
  }
}
