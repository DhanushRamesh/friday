import 'package:flutter/material.dart';

import '../tokens.dart';

/// AppSurface : A bordered panel.
///
/// The one way a box is drawn in the assistant, so that panels cannot drift apart
/// into slightly different greys and radii.
class AppSurface extends StatelessWidget {
  const AppSurface({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(AppSpacing.lg),
    this.raised = false,
    this.radius = AppRadius.md,
    this.borderColor,
    this.width,
  });

  final Widget child;
  final EdgeInsetsGeometry padding;

  /// raised : Whether this sits above another surface, such as a card on
  /// the background or a menu over a panel.
  final bool raised;

  final double radius;
  final Color? borderColor;
  final double? width;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      width: width,
      padding: padding,
      decoration: BoxDecoration(
        color: raised ? colors.surfaceRaised : colors.surface,
        border: Border.all(color: borderColor ?? colors.border),
        borderRadius: BorderRadius.circular(radius),
      ),
      child: child,
    );
  }
}

/// AppWordmark : the assistant's name, set as a logotype.
///
/// A dot in the accent colour stands in for a mark. It doubles as the state
/// light: [listening] makes it pulse, which is the one place the interface
/// says the assistant is paying attention.
class AppWordmark extends StatelessWidget {
  const AppWordmark({super.key, this.size = 20, this.showDot = true});

  final double size;
  final bool showDot;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (showDot) ...[
          Container(
            width: size * 0.4,
            height: size * 0.4,
            decoration: BoxDecoration(
              color: colors.accent,
              shape: BoxShape.circle,
            ),
          ),
          SizedBox(width: size * 0.4),
        ],
        Text(
          'Assistant',
          style: TextStyle(
            fontSize: size,
            height: 1,
            fontWeight: FontWeight.w700,
            letterSpacing: size * 0.13,
            color: colors.textPrimary,
          ),
        ),
      ],
    );
  }
}

/// AppDivider : A one-pixel rule in the border colour.
class AppDivider extends StatelessWidget {
  const AppDivider({super.key, this.vertical = false});

  final bool vertical;

  @override
  Widget build(BuildContext context) => vertical
      ? VerticalDivider(width: 1, thickness: 1, color: context.colors.border)
      : Divider(height: 1, thickness: 1, color: context.colors.border);
}

/// AppEmptyState : What a screen shows when it has nothing to show.
///
/// Says what would go here and how to put something there, rather than
/// leaving a blank that reads as broken.
class AppEmptyState extends StatelessWidget {
  const AppEmptyState({
    super.key,
    required this.title,
    this.body,
    this.icon,
    this.action,
  });

  final String title;
  final String? body;
  final IconData? icon;
  final Widget? action;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.xl),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 30, color: context.colors.textMuted),
            const SizedBox(height: AppSpacing.md),
          ],
          Text(
            title,
            textAlign: TextAlign.center,
            style: context.text.subtitle,
          ),
          if (body != null) ...[
            const SizedBox(height: AppSpacing.sm),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 320),
              child: Text(
                body!,
                textAlign: TextAlign.center,
                style: context.text.caption,
              ),
            ),
          ],
          if (action != null) ...[const SizedBox(height: AppSpacing.lg), action!],
        ],
      ),
    ),
  );
}
