import 'package:flutter/material.dart';

import '../../api/client.dart';
import '../tokens.dart';
import 'spinner.dart';

/// AppTimelineButton : The quiet control that reveals how an answer was made.
///
/// Closed by default and almost invisible. An answer is the thing somebody
/// came for; how it was arrived at matters only when it looks wrong.
class AppTimelineButton extends StatefulWidget {
  const AppTimelineButton({
    super.key,
    required this.chatId,
    required this.load,
  });

  final String chatId;

  /// load : Fetches the timeline. Passed in rather than reaching for the API,
  /// so this widget can be shown a fixed one in a test.
  final Future<AnswerTimeline> Function(String chatId) load;

  @override
  State<AppTimelineButton> createState() => _AppTimelineButtonState();
}

class _AppTimelineButtonState extends State<AppTimelineButton> {
  bool _open = false;
  bool _loading = false;
  AnswerTimeline? _timeline;
  String _failed = '';

  Future<void> _toggle() async {
    if (_open) {
      setState(() => _open = false);
      return;
    }

    setState(() => _open = true);
    if (_timeline != null || _loading) return;

    setState(() {
      _loading = true;
      _failed = '';
    });
    try {
      final got = await widget.load(widget.chatId);
      if (mounted) setState(() => _timeline = got);
    } catch (e) {
      // Said plainly rather than swallowed. This is the screen somebody
      // opened because something already looked wrong.
      if (mounted) setState(() => _failed = '$e');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Semantics(
          button: true,
          label: _open
              ? 'Hide how this answer was made'
              : 'How this answer was made',
          child: InkWell(
            onTap: _toggle,
            borderRadius: BorderRadius.circular(AppRadius.xs),
            child: Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.xxs,
                vertical: AppSpacing.xxs,
              ),
              child: Icon(
                _open ? Icons.info : Icons.info_outline,
                size: 15,
                color: _open ? colors.accent : colors.textMuted,
              ),
            ),
          ),
        ),
        if (_open)
          Padding(
            padding: const EdgeInsets.only(top: AppSpacing.xs),
            child: _Panel(
              loading: _loading,
              failed: _failed,
              timeline: _timeline,
            ),
          ),
      ],
    );
  }
}

/// _Panel : The timeline itself.
class _Panel extends StatelessWidget {
  const _Panel({
    required this.loading,
    required this.failed,
    required this.timeline,
  });

  final bool loading;
  final String failed;
  final AnswerTimeline? timeline;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(AppSpacing.md),
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        borderRadius: BorderRadius.circular(AppRadius.sm),
        border: Border.all(color: colors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text('How this answer was made', style: context.text.label),
              const Spacer(),
              if (timeline != null && timeline!.tookMs > 0)
                Text(_ms(timeline!.tookMs), style: context.text.caption),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          if (loading) const AppThinkingDots(),
          if (failed.isNotEmpty)
            Text(
              failed,
              style: context.text.caption.copyWith(color: colors.danger),
            ),
          if (timeline != null) ..._body(context, timeline!),
        ],
      ),
    );
  }

  List<Widget> _body(BuildContext context, AnswerTimeline t) {
    if (!t.complete && t.steps.isEmpty) {
      return [
        Text(
          'This answer is from before the server kept a record of how answers '
          'were made, so there is nothing to show.',
          style: context.text.caption.copyWith(color: context.colors.textMuted),
        ),
      ];
    }
    return [for (final step in t.steps) _StepRow(step: step)];
  }
}

/// _StepRow : One line of the timeline, with its time down the left.
class _StepRow extends StatelessWidget {
  const _StepRow({required this.step});

  final AnswerStep step;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;

    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 52,
            child: Text(
              step.offsetMs == null ? '' : _ms(step.offsetMs!),
              textAlign: TextAlign.right,
              style: context.text.caption.copyWith(color: colors.textMuted),
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(
                      _icon(step.kind),
                      size: 13,
                      color: _tint(context, step),
                    ),
                    const SizedBox(width: AppSpacing.xs),
                    Flexible(
                      child: Text(
                        _title(step),
                        style: context.text.caption.copyWith(
                          color: _tint(context, step),
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    if (step.tookMs > 0) ...[
                      const SizedBox(width: AppSpacing.xs),
                      Text(
                        '· ${_ms(step.tookMs)}',
                        style: context.text.caption.copyWith(
                          color: colors.textMuted,
                        ),
                      ),
                    ],
                  ],
                ),
                ..._detail(context, step),
              ],
            ),
          ),
        ],
      ),
    );
  }

  List<Widget> _detail(BuildContext context, AnswerStep s) {
    switch (s.kind) {
      case AnswerStepKind.recalled:
        return _recalled(context, s);
      case AnswerStepKind.toolCall:
        return [if (s.arguments.isNotEmpty) _Mono(text: s.arguments)];
      case AnswerStepKind.toolResult:
        return [if (s.content.isNotEmpty) _Quiet(text: s.content)];
      case AnswerStepKind.failed:
        return [
          if (s.text.isNotEmpty) _Quiet(text: s.text),
          if (s.detail.isNotEmpty) _Mono(text: s.detail),
        ];
      default:
        return [if (s.text.isNotEmpty) _Quiet(text: s.text)];
    }
  }

  List<Widget> _recalled(BuildContext context, AnswerStep s) {
    final r = s.recalled;
    if (r == null || r.isEmpty) {
      return [_Quiet(text: 'Nothing was recalled.')];
    }

    return [
      for (final n in r.always) _Note(label: 'always', text: n.text),
      for (final n in r.notes)
        _Note(label: n.score.toStringAsFixed(3), text: n.text),
      for (final e in r.exchanges)
        _Note(label: e.score.toStringAsFixed(3), text: e.text, said: true),
      if (s.byWords)
        Padding(
          padding: const EdgeInsets.only(top: AppSpacing.xxs),
          child: Text(
            'Matched by wording, not meaning: the embedding server was not answering.',
            style: context.text.caption.copyWith(color: context.colors.warning),
          ),
        ),
    ];
  }

  static IconData _icon(AnswerStepKind kind) => switch (kind) {
    AnswerStepKind.asked => Icons.north_east,
    AnswerStepKind.recalled => Icons.psychology_outlined,
    AnswerStepKind.toolCall => Icons.build_outlined,
    AnswerStepKind.toolResult => Icons.subdirectory_arrow_right,
    AnswerStepKind.answered => Icons.south_west,
    AnswerStepKind.failed => Icons.error_outline,
    AnswerStepKind.unknown => Icons.circle_outlined,
  };

  static String _title(AnswerStep s) => switch (s.kind) {
    AnswerStepKind.asked => 'You asked',
    AnswerStepKind.recalled => 'Recalled',
    AnswerStepKind.toolCall => 'Called ${s.name}',
    AnswerStepKind.toolResult => '${s.name} ${s.outcome}',
    AnswerStepKind.answered => 'Answered',
    AnswerStepKind.failed => 'Failed',
    AnswerStepKind.unknown => 'Something happened',
  };

  static Color _tint(BuildContext context, AnswerStep s) {
    final colors = context.colors;
    if (s.kind == AnswerStepKind.failed) return colors.danger;
    if (s.kind == AnswerStepKind.toolResult) {
      return switch (s.outcome) {
        'ok' => colors.success,
        'partial' => colors.warning,
        'failed' => colors.danger,
        _ => colors.textSecondary,
      };
    }
    return colors.textSecondary;
  }
}

/// _Note : One recalled memory or exchange, with what it scored.
class _Note extends StatelessWidget {
  const _Note({required this.label, required this.text, this.said = false});

  final String label;
  final String text;

  /// said : Whether this came from the transcript rather than from a
  /// memory. They are different claims and are not shown as one thing.
  final bool said;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.xxs),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
            decoration: BoxDecoration(
              color: said ? colors.surface : colors.accentSoft,
              borderRadius: BorderRadius.circular(AppRadius.xs),
            ),
            child: Text(
              label,
              style: context.text.caption.copyWith(
                color: said ? colors.textMuted : colors.accent,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Text(
              text,
              style: context.text.caption.copyWith(color: colors.textSecondary),
            ),
          ),
        ],
      ),
    );
  }
}

/// _Quiet : Text belonging to a step, set back from it.
class _Quiet extends StatelessWidget {
  const _Quiet({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: AppSpacing.xxs),
    child: Text(
      text,
      style: context.text.caption.copyWith(color: context.colors.textSecondary),
    ),
  );
}

/// _Mono : What was sent or returned verbatim, which is worth not reflowing.
class _Mono extends StatelessWidget {
  const _Mono({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: AppSpacing.xxs),
    child: SelectableText(
      text,
      style: context.text.caption.copyWith(
        fontFamily: 'monospace',
        color: context.colors.textMuted,
      ),
    ),
  );
}

/// _ms : A duration, in whatever unit reads best at that size.
String _ms(int ms) =>
    ms < 1000 ? '${ms}ms' : '${(ms / 1000).toStringAsFixed(1)}s';
