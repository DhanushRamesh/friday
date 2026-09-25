import 'package:flutter/material.dart';

import '../tokens.dart';
import 'button.dart';

/// AppBannerTone : How serious the message is.
enum AppBannerTone { info, warning, error }

/// AppBanner : An inline message about the whole screen, such as a login
/// that failed or a server that cannot be reached.
///
/// Inline rather than a snackbar: what it says usually needs acting on, and
/// a message that disappears on its own is one the user may never have seen.
class AppBanner extends StatelessWidget {
  const AppBanner({
    super.key,
    required this.message,
    this.tone = AppBannerTone.error,
    this.actionLabel,
    this.onAction,
  });

  final String message;
  final AppBannerTone tone;

  /// actionLabel : The recovery, such as "Try again". Shown only with
  /// [onAction].
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (accent, background, icon) = switch (tone) {
      AppBannerTone.info => (
        colors.accent,
        colors.accentSoft,
        Icons.info_outline,
      ),
      AppBannerTone.warning => (
        colors.warning,
        colors.accentSoft,
        Icons.warning_amber_outlined,
      ),
      AppBannerTone.error => (
        colors.danger,
        colors.dangerSoft,
        Icons.error_outline,
      ),
    };

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(AppSpacing.md),
      decoration: BoxDecoration(
        color: background,
        border: Border.all(color: accent.withValues(alpha: 0.35)),
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 17, color: accent),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Text(
              message,
              style: context.text.body.copyWith(color: colors.textPrimary),
            ),
          ),
          if (onAction != null && actionLabel != null) ...[
            const SizedBox(width: AppSpacing.sm),
            AppButton(
              label: actionLabel!,
              onPressed: onAction,
              variant: AppButtonVariant.secondary,
              compact: true,
            ),
          ],
        ],
      ),
    );
  }
}
