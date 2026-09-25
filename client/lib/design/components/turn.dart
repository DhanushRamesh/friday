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
    this.superseded = false,
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

  /// superseded : Whether a later prompt cancelled this one. Shown faded
  /// rather than removed, so the conversation still reads in order and the
  /// user can see what was dropped.
  final bool superseded;

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
                  SelectableText(text, style: style),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
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
