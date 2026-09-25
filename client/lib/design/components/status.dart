import 'package:flutter/material.dart';

import '../../api/models.dart' show ChatStatus;
import '../tokens.dart';

/// AppStatusDot : A chat's status as a coloured dot, optionally named.
///
/// Colour alone would be unreadable to anyone who cannot distinguish it, so
/// the label is on by default and the dot is the decoration.
class AppStatusDot extends StatelessWidget {
  const AppStatusDot({super.key, required this.status, this.showLabel = true});

  final ChatStatus status;
  final bool showLabel;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (color, label) = switch (status) {
      ChatStatus.pending => (colors.textMuted, 'Queued'),
      ChatStatus.running => (colors.accent, 'Working'),
      ChatStatus.completed => (colors.success, 'Answered'),
      ChatStatus.failed => (colors.danger, 'Failed'),
      ChatStatus.cancelled => (colors.textMuted, 'Stopped'),
      ChatStatus.unknown => (colors.textMuted, 'Unknown'),
    };

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 6,
          height: 6,
          decoration: BoxDecoration(color: color, shape: BoxShape.circle),
        ),
        if (showLabel) ...[
          const SizedBox(width: AppSpacing.xs + 2),
          Text(label, style: context.text.caption.copyWith(color: color)),
        ],
      ],
    );
  }
}
