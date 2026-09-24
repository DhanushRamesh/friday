import 'package:flutter/material.dart';

import '../tokens.dart';

/// FSessionTile : One session in the sidebar.
///
/// A session usually has no title — it is whatever was said in it — so an
/// untitled one falls back to its first prompt rather than showing an
/// identifier nobody can read.
class FSessionTile extends StatefulWidget {
  const FSessionTile({
    super.key,
    required this.title,
    required this.selected,
    required this.onTap,
    this.subtitle,
    this.active = false,
  });

  final String title;

  /// selected : Whether this is the session being looked at.
  final bool selected;

  /// active : Whether this is where a prompt from this client would land.
  /// Usually the same as [selected], but not while browsing another.
  final bool active;

  final String? subtitle;
  final VoidCallback onTap;

  @override
  State<FSessionTile> createState() => _FSessionTileState();
}

class _FSessionTileState extends State<FSessionTile> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final background = widget.selected
        ? colors.accentSoft
        : _hovered
        ? colors.surfaceRaised
        : Colors.transparent;

    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        onTap: widget.onTap,
        behavior: HitTestBehavior.opaque,
        child: AnimatedContainer(
          duration: FMotion.fast,
          padding: const EdgeInsets.symmetric(
            horizontal: FSpacing.md,
            vertical: FSpacing.sm + 2,
          ),
          decoration: BoxDecoration(
            color: background,
            borderRadius: BorderRadius.circular(FRadius.sm),
          ),
          child: Row(
            children: [
              // Marks where a prompt would land, which is not always the
              // session on screen.
              Container(
                width: 5,
                height: 5,
                margin: const EdgeInsets.only(right: FSpacing.sm),
                decoration: BoxDecoration(
                  color: widget.active ? colors.accent : Colors.transparent,
                  shape: BoxShape.circle,
                ),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      widget.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: context.text.body.copyWith(
                        fontWeight: widget.selected
                            ? FontWeight.w600
                            : FontWeight.w400,
                        color: widget.selected
                            ? colors.textPrimary
                            : colors.textSecondary,
                      ),
                    ),
                    if (widget.subtitle != null) ...[
                      const SizedBox(height: 1),
                      Text(
                        widget.subtitle!,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: context.text.caption,
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
