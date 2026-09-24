/// The values every FRIDAY screen is built from.
///
/// Nothing in the UI hard-codes a colour, a gap or a radius. They live here
/// so that the whole app changes together, and so that a screen can be read
/// without wondering whether a particular grey was deliberate.
///
/// Reached through the theme rather than imported directly —
/// `context.friday` — so that light and dark differ only in the values, not
/// in the widgets that use them.
library;

import 'package:flutter/material.dart';

/// FSpacing : The gaps. A small scale, because a large one only invites
/// two spacings that differ by a pixel and mean nothing.
abstract final class FSpacing {
  static const double xxs = 2;
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;
  static const double xxl = 32;
  static const double xxxl = 48;
}

/// FRadius : The corner radii.
abstract final class FRadius {
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;

  /// pill : Fully rounded, for anything shaped like a tablet or a dot.
  static const double pill = 999;
}

/// FMotion : How long things take.
///
/// Short, and shorter still for anything that happens while the user is
/// speaking or listening: an animation that outlasts the moment it explains
/// is just a delay.
abstract final class FMotion {
  static const Duration fast = Duration(milliseconds: 120);
  static const Duration base = Duration(milliseconds: 200);
  static const Duration slow = Duration(milliseconds: 320);

  /// curve : Decelerating, so movement settles rather than stopping dead.
  static const Curve curve = Curves.easeOutCubic;
}

/// FBreakpoints : Where the layout changes shape.
abstract final class FBreakpoints {
  /// compact : At or below this the sidebar becomes a drawer, because a
  /// phone has no room for both it and a readable conversation.
  static const double compact = 720;
}

/// FPalette : Every colour a screen may use.
///
/// Named by role rather than by hue, so that a component asks for the border
/// colour rather than for a particular grey and keeps working when the
/// theme changes.
@immutable
class FPalette {
  const FPalette({
    required this.background,
    required this.surface,
    required this.surfaceRaised,
    required this.surfaceSunken,
    required this.border,
    required this.borderStrong,
    required this.textPrimary,
    required this.textSecondary,
    required this.textMuted,
    required this.accent,
    required this.accentText,
    required this.accentSoft,
    required this.danger,
    required this.dangerSoft,
    required this.success,
    required this.warning,
    required this.overlay,
  });

  /// background : Behind everything.
  final Color background;

  /// surface : A panel sitting on the background, such as the sidebar.
  final Color surface;

  /// surfaceRaised : A card or menu sitting above a surface.
  final Color surfaceRaised;

  /// surfaceSunken : An input or a well, reading as recessed.
  final Color surfaceSunken;

  final Color border;

  /// borderStrong : For a focused or selected edge.
  final Color borderStrong;

  final Color textPrimary;
  final Color textSecondary;

  /// textMuted : Timestamps and hints — present but not competing.
  final Color textMuted;

  /// accent : FRIDAY's own colour, used sparingly so that it still means
  /// something when it appears.
  final Color accent;

  /// accentText : What is legible on top of [accent].
  final Color accentText;

  /// accentSoft : A tint of the accent, for a background rather than a mark.
  final Color accentSoft;

  final Color danger;
  final Color dangerSoft;
  final Color success;
  final Color warning;

  /// overlay : Dims what is behind a dialog or a drawer.
  final Color overlay;

  /// dark : The default. FRIDAY is used at night and on a phone, and a dark
  /// field keeps a long answer from glaring.
  static const FPalette dark = FPalette(
    background: Color(0xFF0E1113),
    surface: Color(0xFF15191C),
    surfaceRaised: Color(0xFF1C2126),
    surfaceSunken: Color(0xFF0A0D0F),
    border: Color(0xFF262C32),
    borderStrong: Color(0xFF3A434B),
    textPrimary: Color(0xFFE8EDF1),
    textSecondary: Color(0xFFA3AEB7),
    textMuted: Color(0xFF6C7780),
    accent: Color(0xFFE8A33D),
    accentText: Color(0xFF1A1206),
    accentSoft: Color(0x1FE8A33D),
    danger: Color(0xFFE5645C),
    dangerSoft: Color(0x1FE5645C),
    success: Color(0xFF55B889),
    warning: Color(0xFFD9A343),
    overlay: Color(0x99000000),
  );

  /// light : For a desktop browser in a bright room.
  static const FPalette light = FPalette(
    background: Color(0xFFF6F7F9),
    surface: Color(0xFFFFFFFF),
    surfaceRaised: Color(0xFFFFFFFF),
    surfaceSunken: Color(0xFFEFF1F4),
    border: Color(0xFFDFE3E8),
    borderStrong: Color(0xFFB9C1CA),
    textPrimary: Color(0xFF13181C),
    textSecondary: Color(0xFF4C565F),
    textMuted: Color(0xFF7B858E),
    accent: Color(0xFFB0721A),
    accentText: Color(0xFFFFFFFF),
    accentSoft: Color(0x1FB0721A),
    danger: Color(0xFFC33C33),
    dangerSoft: Color(0x1FC33C33),
    success: Color(0xFF2E8B5F),
    warning: Color(0xFF9A6C14),
    overlay: Color(0x44000000),
  );
}

/// FTypography : The text styles, by the job each does.
@immutable
class FTypography {
  const FTypography({
    required this.display,
    required this.title,
    required this.subtitle,
    required this.body,
    required this.bodyStrong,
    required this.caption,
    required this.label,
    required this.mono,
  });

  /// display : The one large thing on a screen, such as the login heading.
  final TextStyle display;

  final TextStyle title;
  final TextStyle subtitle;

  /// body : What almost everything is, including an answer. Set at a size
  /// and line height meant to be read in long runs rather than skimmed.
  final TextStyle body;

  final TextStyle bodyStrong;

  /// caption : Timestamps and secondary detail.
  final TextStyle caption;

  /// label : Small, uppercase, wide — a field label or a section heading.
  final TextStyle label;

  /// mono : Anything FRIDAY returns that is code or an identifier.
  final TextStyle mono;

  /// of : The styles, coloured for a palette.
  factory FTypography.of(FPalette palette) {
    // The machine's own font, never a downloaded one.
    //
    // Left unset, Flutter web defaults to Roboto and fetches it from
    // fonts.gstatic.com on every load. That is a request to Google on
    // behalf of whoever opens FRIDAY, and it fails outright on a network
    // that cannot reach them — which is how this was found. system-ui
    // resolves to whatever the device already has.
    const primary = 'system-ui';
    const family = <String>[
      '-apple-system',
      'BlinkMacSystemFont',
      'Segoe UI',
      'Roboto',
      'Helvetica Neue',
      'Arial',
      'sans-serif',
    ];
    return FTypography(
      display: TextStyle(
        fontSize: 27,
        height: 1.25,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.4,
        color: palette.textPrimary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      title: TextStyle(
        fontSize: 19,
        height: 1.3,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.2,
        color: palette.textPrimary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      subtitle: TextStyle(
        fontSize: 15,
        height: 1.35,
        fontWeight: FontWeight.w600,
        color: palette.textPrimary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      body: TextStyle(
        fontSize: 15,
        height: 1.55,
        fontWeight: FontWeight.w400,
        color: palette.textPrimary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      bodyStrong: TextStyle(
        fontSize: 15,
        height: 1.55,
        fontWeight: FontWeight.w500,
        color: palette.textPrimary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      caption: TextStyle(
        fontSize: 12.5,
        height: 1.4,
        fontWeight: FontWeight.w400,
        color: palette.textMuted,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      label: TextStyle(
        fontSize: 11,
        height: 1.3,
        fontWeight: FontWeight.w600,
        letterSpacing: 0.7,
        color: palette.textSecondary,
        fontFamily: primary,
        fontFamilyFallback: family,
      ),
      mono: TextStyle(
        fontSize: 13,
        height: 1.5,
        fontWeight: FontWeight.w400,
        color: palette.textPrimary,
        fontFamily: 'monospace',
        fontFamilyFallback: const ['Menlo', 'Consolas', 'monospace'],
      ),
    );
  }
}

/// FTokens : The palette and type, carried on the theme.
///
/// A ThemeExtension rather than globals, so that a widget reads the values
/// for the theme it is actually under — which is what lets the component
/// gallery show light and dark side by side.
@immutable
class FTokens extends ThemeExtension<FTokens> {
  const FTokens({required this.palette, required this.typography});

  final FPalette palette;

  /// typography : Named in full rather than `type`, which ThemeExtension
  /// already declares and Flutter uses as the key an extension is looked up
  /// by. Shadowing it compiles and then fails to find the tokens at all.
  final FTypography typography;

  /// of : The tokens for the theme in scope.
  static FTokens of(BuildContext context) =>
      Theme.of(context).extension<FTokens>() ??
      FTokens(
        palette: FPalette.dark,
        typography: FTypography.of(FPalette.dark),
      );

  @override
  FTokens copyWith({FPalette? palette, FTypography? typography}) => FTokens(
    palette: palette ?? this.palette,
    typography: typography ?? this.typography,
  );

  /// lerp : Required by ThemeExtension. The palette is swapped rather than
  /// blended, because a half-way colour between the two themes is a colour
  /// neither was designed with.
  @override
  FTokens lerp(ThemeExtension<FTokens>? other, double t) {
    if (other is! FTokens) return this;
    return t < 0.5 ? this : other;
  }
}

/// FContext : Reaching the tokens without naming the theme each time.
extension FContext on BuildContext {
  /// friday : The tokens in scope.
  FTokens get friday => FTokens.of(this);

  /// colors : The palette in scope.
  FPalette get colors => FTokens.of(this).palette;

  /// text : The type in scope.
  FTypography get text => FTokens.of(this).typography;

  /// isCompact : Whether the window is too narrow for a sidebar beside the
  /// conversation.
  bool get isCompact => MediaQuery.sizeOf(this).width <= FBreakpoints.compact;
}
