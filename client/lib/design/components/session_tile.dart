import 'package:flutter/material.dart';

import '../tokens.dart';

/// AppSessionTile : One session in the sidebar.
///
/// A session usually has no title — it is whatever was said in it — so an
/// untitled one falls back to its first prompt rather than showing an
/// identifier nobody can read.
class AppSessionTile extends StatefulWidget {
  const AppSessionTile({
    super.key,
    required this.title,
    required this.selected,
    required this.onTap,
    this.subtitle,
    this.active = false,
    this.archived = false,
    this.onRename,
    this.onArchive,
    this.onDelete,
  });

  final String title;

  /// selected : Whether this is the session being looked at.
  final bool selected;

  /// active : Whether this is where a prompt from this client would land.
  /// Usually the same as [selected], but not while browsing another.
  final bool active;

  final String? subtitle;
  final VoidCallback onTap;

  /// archived : Whether this session has been put away, which decides
  /// whether the menu offers to archive it or to bring it back.
  final bool archived;

  /// onRename, onArchive, onDelete : The actions the menu offers. A null one
  /// is left out rather than shown disabled.
  final VoidCallback? onRename;
  final VoidCallback? onArchive;
  final VoidCallback? onDelete;

  @override
  State<AppSessionTile> createState() => _FSessionTileState();
}

class _FSessionTileState extends State<AppSessionTile> {
  bool _hovered = false;

  bool get _hasActions =>
      widget.onRename != null ||
      widget.onArchive != null ||
      widget.onDelete != null;

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
          duration: AppMotion.fast,
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.md,
            vertical: AppSpacing.sm + 2,
          ),
          decoration: BoxDecoration(
            color: background,
            borderRadius: BorderRadius.circular(AppRadius.sm),
          ),
          child: Row(
            children: [
              // Marks where a prompt would land, which is not always the
              // session on screen.
              Container(
                width: 5,
                height: 5,
                margin: const EdgeInsets.only(right: AppSpacing.sm),
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
              if (_hasActions && (_hovered || widget.selected))
                _TileMenu(
                  archived: widget.archived,
                  onRename: widget.onRename,
                  onArchive: widget.onArchive,
                  onDelete: widget.onDelete,
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// _TileMenu : Rename, archive and delete for one session.
///
/// Shown on hover or while the session is selected, rather than always: a
/// column of sessions each carrying visible buttons is a column of buttons
/// with names attached.
class _TileMenu extends StatelessWidget {
  const _TileMenu({
    required this.archived,
    this.onRename,
    this.onArchive,
    this.onDelete,
  });

  final bool archived;
  final VoidCallback? onRename;
  final VoidCallback? onArchive;
  final VoidCallback? onDelete;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return PopupMenuButton<VoidCallback>(
      tooltip: 'More',
      padding: EdgeInsets.zero,
      splashRadius: 14,
      color: colors.surfaceRaised,
      icon: Icon(Icons.more_horiz, size: 16, color: colors.textMuted),
      // The tile underneath is what switches session, and a tap that lands on
      // it while aiming for the menu would navigate away instead.
      onSelected: (action) => action(),
      itemBuilder: (context) => [
        if (onRename != null)
          _item(context, Icons.edit_outlined, 'Rename', onRename!),
        if (onArchive != null)
          _item(
            context,
            archived ? Icons.unarchive_outlined : Icons.archive_outlined,
            archived ? 'Unarchive' : 'Archive',
            onArchive!,
          ),
        if (onDelete != null)
          _item(
            context,
            Icons.delete_outline,
            'Delete',
            onDelete!,
            tone: colors.danger,
          ),
      ],
    );
  }

  PopupMenuItem<VoidCallback> _item(
    BuildContext context,
    IconData icon,
    String label,
    VoidCallback action, {
    Color? tone,
  }) {
    final colour = tone ?? context.colors.textSecondary;
    return PopupMenuItem<VoidCallback>(
      value: action,
      height: 38,
      child: Row(
        children: [
          Icon(icon, size: 15, color: colour),
          const SizedBox(width: AppSpacing.sm),
          Text(label, style: context.text.body.copyWith(color: colour)),
        ],
      ),
    );
  }
}
