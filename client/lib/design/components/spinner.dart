import 'package:flutter/material.dart';

import '../tokens.dart';

/// AppSpinner : A small indeterminate spinner in the accent colour.
class AppSpinner extends StatelessWidget {
  const AppSpinner({super.key, this.size = 16, this.color});

  final double size;

  /// color : Overrides the accent, for a spinner sitting on the accent
  /// itself, such as inside a primary button.
  final Color? color;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: size,
    height: size,
    child: CircularProgressIndicator(
      strokeWidth: size <= 16 ? 2 : 2.5,
      valueColor: AlwaysStoppedAnimation(color ?? context.colors.accent),
    ),
  );
}

/// AppThinkingDots : Three dots that rise in turn, shown while the assistant is
/// working and has not said anything yet.
///
/// A voice assistant that goes quiet is indistinguishable from one that has
/// crashed, so the silence before the first transient message needs filling.
/// It stops as soon as there is something to show.
class AppThinkingDots extends StatefulWidget {
  const AppThinkingDots({super.key, this.size = 5});

  final double size;

  @override
  State<AppThinkingDots> createState() => _FThinkingDotsState();
}

class _FThinkingDotsState extends State<AppThinkingDots>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1100),
  )..repeat();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) => Row(
        mainAxisSize: MainAxisSize.min,
        children: List.generate(3, (i) {
          // Each dot is a third of a cycle behind the one before it.
          final phase = (_controller.value - i * 0.18) % 1.0;
          final lift = phase < 0.5 ? Curves.easeOut.transform(phase * 2) : 0.0;
          return Padding(
            padding: EdgeInsets.only(right: i == 2 ? 0 : widget.size * 0.8),
            child: Transform.translate(
              offset: Offset(0, -lift * widget.size * 0.9),
              child: Container(
                width: widget.size,
                height: widget.size,
                decoration: BoxDecoration(
                  color: context.colors.textMuted.withValues(
                    alpha: 0.45 + lift * 0.55,
                  ),
                  shape: BoxShape.circle,
                ),
              ),
            ),
          );
        }),
      ),
    );
  }
}
