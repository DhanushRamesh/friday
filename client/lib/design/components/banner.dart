import 'package:flutter/material.dart';

import '../tokens.dart';
import 'button.dart';

/// FBannerTone : How serious the message is.
enum FBannerTone { info, warning, error }

/// FBanner : An inline message about the whole screen, such as a login
/// that failed or a server that cannot be reached.
///
/// Inline rather than a snackbar: what it says usually needs acting on, and
/// a message that disappears on its own is one the user may never have seen.
class FBanner extends StatelessWidget {
  const FBanner({
    super.key,
    required this.message,
    this.tone = FBannerTone.error,
    this.actionLabel,
    this.onAction,
  });

  final String message;
  final FBannerTone tone;

  /// actionLabel : The recovery, such as "Try again". Shown only with
  /// [onAction].
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (accent, background, icon) = switch (tone) {
      FBannerTone.info => (
        colors.accent,
        colors.accentSoft,
        Icons.info_outline,
      ),
      FBannerTone.warning => (
        colors.warning,
        colors.accentSoft,
        Icons.warning_amber_outlined,
      ),
      FBannerTone.error => (
        colors.danger,
        colors.dangerSoft,
        Icons.error_outline,
      ),
    };

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(FSpacing.md),
      decoration: BoxDecoration(
        color: background,
        border: Border.all(color: accent.withValues(alpha: 0.35)),
        borderRadius: BorderRadius.circular(FRadius.sm),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 17, color: accent),
          const SizedBox(width: FSpacing.sm),
          Expanded(
            child: Text(
              message,
              style: context.text.body.copyWith(color: colors.textPrimary),
            ),
          ),
          if (onAction != null && actionLabel != null) ...[
            const SizedBox(width: FSpacing.sm),
            FButton(
              label: actionLabel!,
              onPressed: onAction,
              variant: FButtonVariant.secondary,
              compact: true,
            ),
          ],
        ],
      ),
    );
  }
}
