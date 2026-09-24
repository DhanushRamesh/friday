import 'package:flutter/material.dart';

import '../tokens.dart';
import 'spinner.dart';

/// FButtonVariant : How much weight a button carries.
enum FButtonVariant {
  /// primary : The one action a screen is for. At most one per view, or the
  /// accent stops meaning anything.
  primary,

  /// secondary : An alternative that is still a real action.
  secondary,

  /// ghost : A quiet action, such as one in a toolbar.
  ghost,

  /// danger : Something that cannot be undone, such as revoking a client.
  danger,
}

/// FButton : The only button in FRIDAY.
///
/// Passing null for [onPressed] disables it; setting [busy] disables it too
/// and shows a spinner in place of the label, so that a slow login cannot be
/// submitted twice.
class FButton extends StatefulWidget {
  const FButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.variant = FButtonVariant.primary,
    this.icon,
    this.busy = false,
    this.expand = false,
    this.compact = false,
  });

  final String label;
  final VoidCallback? onPressed;
  final FButtonVariant variant;
  final IconData? icon;

  /// busy : Whether the action is under way.
  final bool busy;

  /// expand : Whether to fill the width available.
  final bool expand;

  /// compact : A smaller button, for a toolbar or a list row.
  final bool compact;

  @override
  State<FButton> createState() => _FButtonState();
}

class _FButtonState extends State<FButton> {
  bool _hovered = false;
  bool _pressed = false;

  bool get _enabled => widget.onPressed != null && !widget.busy;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (background, foreground, border) = _colors(colors);

    final height = widget.compact ? 32.0 : 42.0;
    final padding = widget.compact ? FSpacing.md : FSpacing.lg;

    return Semantics(
      button: true,
      enabled: _enabled,
      label: widget.label,
      child: MouseRegion(
        cursor: _enabled
            ? SystemMouseCursors.click
            : SystemMouseCursors.forbidden,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: GestureDetector(
          onTapDown: _enabled ? (_) => setState(() => _pressed = true) : null,
          onTapUp: _enabled ? (_) => setState(() => _pressed = false) : null,
          onTapCancel: _enabled ? () => setState(() => _pressed = false) : null,
          onTap: _enabled ? widget.onPressed : null,
          child: AnimatedContainer(
            duration: FMotion.fast,
            curve: FMotion.curve,
            height: height,
            width: widget.expand ? double.infinity : null,
            padding: EdgeInsets.symmetric(horizontal: padding),
            decoration: BoxDecoration(
              color: background,
              border: border == null ? null : Border.all(color: border),
              borderRadius: BorderRadius.circular(FRadius.sm),
            ),
            child: Row(
              mainAxisSize: widget.expand ? MainAxisSize.max : MainAxisSize.min,
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                if (widget.busy) ...[
                  FSpinner(size: 14, color: foreground),
                  const SizedBox(width: FSpacing.sm),
                ] else if (widget.icon != null) ...[
                  Icon(
                    widget.icon,
                    size: widget.compact ? 15 : 17,
                    color: foreground,
                  ),
                  const SizedBox(width: FSpacing.sm),
                ],
                Flexible(
                  child: Text(
                    widget.label,
                    overflow: TextOverflow.ellipsis,
                    style:
                        (widget.compact
                                ? context.text.caption
                                : context.text.bodyStrong)
                            .copyWith(
                              color: foreground,
                              fontWeight: FontWeight.w600,
                            ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// _colors : The background, foreground and border for the variant and
  /// the current interaction.
  (Color, Color, Color?) _colors(FPalette c) {
    if (!_enabled) {
      // Disabled reads as flat and low contrast rather than as a different
      // colour, so it is obviously the same control.
      return (
        c.surfaceRaised,
        c.textMuted,
        widget.variant == FButtonVariant.ghost ? null : c.border,
      );
    }

    final lift = _pressed ? -0.06 : (_hovered ? 0.08 : 0.0);
    return switch (widget.variant) {
      FButtonVariant.primary => (_shift(c.accent, lift), c.accentText, null),
      FButtonVariant.secondary => (
        _shift(c.surfaceRaised, lift),
        c.textPrimary,
        _hovered ? c.borderStrong : c.border,
      ),
      FButtonVariant.ghost => (
        _hovered ? c.surfaceRaised : Colors.transparent,
        c.textSecondary,
        null,
      ),
      FButtonVariant.danger => (_shift(c.danger, lift), Colors.white, null),
    };
  }

  /// _shift : Lightens or darkens a colour, for hover and press.
  Color _shift(Color base, double amount) {
    if (amount == 0) return base;
    final hsl = HSLColor.fromColor(base);
    return hsl
        .withLightness((hsl.lightness + amount).clamp(0.0, 1.0))
        .toColor();
  }
}
