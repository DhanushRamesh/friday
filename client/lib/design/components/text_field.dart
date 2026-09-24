import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../tokens.dart';

/// FTextField : A labelled input.
///
/// The error is part of the field rather than a snackbar, because the thing
/// that went wrong and the thing to fix it should be in the same place.
class FTextField extends StatefulWidget {
  const FTextField({
    super.key,
    required this.controller,
    this.label,
    this.hint,
    this.error,
    this.obscure = false,
    this.autofocus = false,
    this.enabled = true,
    this.icon,
    this.keyboardType,
    this.textInputAction,
    this.autofillHints,
    this.onSubmitted,
    this.onChanged,
    this.maxLines = 1,
    this.minLines,
  });

  final TextEditingController controller;
  final String? label;
  final String? hint;

  /// error : Shown beneath the field and marks its border. Null when fine.
  final String? error;

  /// obscure : Whether this holds a password. Adds a reveal toggle, since a
  /// password typed wrongly on a phone is the usual reason a login fails.
  final bool obscure;

  final bool autofocus;
  final bool enabled;
  final IconData? icon;
  final TextInputType? keyboardType;
  final TextInputAction? textInputAction;
  final Iterable<String>? autofillHints;
  final ValueChanged<String>? onSubmitted;
  final ValueChanged<String>? onChanged;
  final int maxLines;
  final int? minLines;

  @override
  State<FTextField> createState() => _FTextFieldState();
}

class _FTextFieldState extends State<FTextField> {
  final FocusNode _focus = FocusNode();
  bool _revealed = false;

  @override
  void initState() {
    super.initState();
    _focus.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final hasError = widget.error != null && widget.error!.isNotEmpty;

    final borderColor = hasError
        ? colors.danger
        : _focus.hasFocus
        ? colors.accent
        : colors.border;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (widget.label != null) ...[
          Text(widget.label!.toUpperCase(), style: context.text.label),
          const SizedBox(height: FSpacing.sm),
        ],
        AnimatedContainer(
          duration: FMotion.fast,
          decoration: BoxDecoration(
            color: widget.enabled ? colors.surfaceSunken : colors.surface,
            border: Border.all(color: borderColor, width: 1.5),
            borderRadius: BorderRadius.circular(FRadius.sm),
          ),
          padding: const EdgeInsets.symmetric(horizontal: FSpacing.md),
          child: Row(
            children: [
              if (widget.icon != null) ...[
                Icon(widget.icon, size: 17, color: colors.textMuted),
                const SizedBox(width: FSpacing.sm),
              ],
              Expanded(
                child: TextField(
                  controller: widget.controller,
                  focusNode: _focus,
                  enabled: widget.enabled,
                  obscureText: widget.obscure && !_revealed,
                  autofocus: widget.autofocus,
                  keyboardType: widget.keyboardType,
                  textInputAction: widget.textInputAction,
                  autofillHints: widget.autofillHints,
                  onSubmitted: widget.onSubmitted,
                  onChanged: widget.onChanged,
                  maxLines: widget.obscure ? 1 : widget.maxLines,
                  minLines: widget.minLines,
                  style: context.text.body,
                  cursorColor: colors.accent,
                  decoration: InputDecoration(
                    isDense: true,
                    border: InputBorder.none,
                    hintText: widget.hint,
                    hintStyle: context.text.body.copyWith(
                      color: colors.textMuted,
                    ),
                    contentPadding: const EdgeInsets.symmetric(
                      vertical: FSpacing.md,
                    ),
                  ),
                ),
              ),
              if (widget.obscure)
                _RevealToggle(
                  revealed: _revealed,
                  onTap: () => setState(() => _revealed = !_revealed),
                ),
            ],
          ),
        ),
        if (hasError) ...[
          const SizedBox(height: FSpacing.sm),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(Icons.error_outline, size: 14, color: colors.danger),
              const SizedBox(width: FSpacing.xs),
              Expanded(
                child: Text(
                  widget.error!,
                  style: context.text.caption.copyWith(color: colors.danger),
                ),
              ),
            ],
          ),
        ],
      ],
    );
  }
}

/// _RevealToggle : The eye that shows a password.
class _RevealToggle extends StatelessWidget {
  const _RevealToggle({required this.revealed, required this.onTap});

  final bool revealed;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Semantics(
    button: true,
    label: revealed ? 'Hide password' : 'Show password',
    child: MouseRegion(
      cursor: SystemMouseCursors.click,
      child: GestureDetector(
        onTap: () {
          HapticFeedback.selectionClick();
          onTap();
        },
        behavior: HitTestBehavior.opaque,
        child: Padding(
          padding: const EdgeInsets.all(FSpacing.xs),
          child: Icon(
            revealed
                ? Icons.visibility_off_outlined
                : Icons.visibility_outlined,
            size: 17,
            color: context.colors.textMuted,
          ),
        ),
      ),
    ),
  );
}
