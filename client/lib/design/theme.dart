/// Building Flutter's theme from the assistant's tokens.
library;

import 'package:flutter/material.dart';

import 'tokens.dart';

/// AppTheme : The two themes, built from the two palettes.
///
/// Material's own ThemeData is filled in as well as the tokens, because
/// widgets the assistant does not own — a scrollbar, a text selection handle, a
/// tooltip — read from it and would otherwise arrive in Material's default
/// blue.
abstract final class AppTheme {
  /// dark : The default.
  static ThemeData get dark => _build(AppPalette.dark, Brightness.dark);

  /// light : For a bright room.
  static ThemeData get light => _build(AppPalette.light, Brightness.light);

  static ThemeData _build(AppPalette palette, Brightness brightness) {
    final typography = AppTypography.of(palette);

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      // So that a widget the app does not own — a tooltip, a menu — uses the
      // device's font too, rather than sending Flutter to fetch Roboto.
      fontFamily: 'system-ui',
      fontFamilyFallback: const [
        '-apple-system',
        'BlinkMacSystemFont',
        'Segoe UI',
        'Roboto',
        'Helvetica Neue',
        'Arial',
        'sans-serif',
      ],
      scaffoldBackgroundColor: palette.background,
      canvasColor: palette.background,
      colorScheme: ColorScheme(
        brightness: brightness,
        primary: palette.accent,
        onPrimary: palette.accentText,
        secondary: palette.accent,
        onSecondary: palette.accentText,
        error: palette.danger,
        onError: Colors.white,
        surface: palette.surface,
        onSurface: palette.textPrimary,
        outline: palette.border,
      ),
      extensions: [AppTokens(palette: palette, typography: typography)],
      textTheme: TextTheme(
        headlineMedium: typography.display,
        titleLarge: typography.title,
        titleMedium: typography.subtitle,
        bodyLarge: typography.body,
        bodyMedium: typography.body,
        bodySmall: typography.caption,
        labelSmall: typography.label,
      ),
      dividerTheme: DividerThemeData(
        color: palette.border,
        thickness: 1,
        space: 1,
      ),
      // The cursor and selection are the accent, so that typing a prompt
      // looks like part of the app rather than part of the browser.
      textSelectionTheme: TextSelectionThemeData(
        cursorColor: palette.accent,
        selectionColor: palette.accentSoft,
        selectionHandleColor: palette.accent,
      ),
      scrollbarTheme: ScrollbarThemeData(
        thumbColor: WidgetStatePropertyAll(palette.borderStrong),
        thickness: const WidgetStatePropertyAll(6),
        radius: const Radius.circular(AppRadius.pill),
        crossAxisMargin: 2,
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(
          color: palette.surfaceRaised,
          border: Border.all(color: palette.border),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        textStyle: typography.caption.copyWith(color: palette.textPrimary),
        waitDuration: const Duration(milliseconds: 500),
      ),
      splashFactory: NoSplash.splashFactory,
      highlightColor: Colors.transparent,
    );
  }
}
