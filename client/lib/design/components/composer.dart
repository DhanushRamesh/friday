import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../tokens.dart';
import 'button.dart';

/// AppComposer : Where a prompt is typed.
///
/// Enter sends and shift-enter adds a line, which is what a chat interface
/// is expected to do; the alternative costs a reach for the mouse on every
/// message. The field grows with the text up to a few lines and then
/// scrolls, so a long prompt does not swallow the conversation above it.
class AppComposer extends StatefulWidget {
  const AppComposer({
    super.key,
    required this.controller,
    required this.onSend,
    this.onStop,
    this.busy = false,
    this.enabled = true,
    this.hint = 'Ask the assistant…',
    this.focusNode,
    this.onListen,
    this.listening = false,
    this.heard,
    this.listeningHint,
  });

  final TextEditingController controller;

  /// onSend : Called with the trimmed text. Not called when it is empty.
  final ValueChanged<String> onSend;

  /// onStop : Cancels what is running. When present the send button becomes
  /// a stop button, because the two are never both useful and a single
  /// control in one place is faster to reach.
  final VoidCallback? onStop;

  /// busy : Whether a prompt is on its way to the server.
  final bool busy;

  final bool enabled;
  final String hint;
  final FocusNode? focusNode;

  /// onListen : Opens or closes the microphone. Absent on a device that
  /// cannot listen, which hides the control rather than showing one that
  /// does nothing.
  final VoidCallback? onListen;

  /// listening : Whether the microphone is open.
  final bool listening;

  /// heard : What has been picked up so far, shown in place of the field
  /// while listening. Seeing the words appear is the only sign the
  /// microphone is working.
  final String? heard;

  /// listeningHint : What to show before anything has been heard. Says
  /// what the microphone is waiting for, which differs between holding it
  /// open by hand and waiting to be addressed by name.
  final String? listeningHint;

  @override
  State<AppComposer> createState() => _FComposerState();
}

class _FComposerState extends State<AppComposer> {
  late final FocusNode _focus = widget.focusNode ?? FocusNode();
  bool _ownsFocus = false;
  bool _hasText = false;

  @override
  void initState() {
    super.initState();
    _ownsFocus = widget.focusNode == null;
    _focus.addListener(_onFocusChanged);
    widget.controller.addListener(_onTextChanged);
    _hasText = widget.controller.text.trim().isNotEmpty;
  }

  @override
  void dispose() {
    widget.controller.removeListener(_onTextChanged);
    _focus.removeListener(_onFocusChanged);
    if (_ownsFocus) _focus.dispose();
    super.dispose();
  }

  void _onFocusChanged() => setState(() {});

  void _onTextChanged() {
    final has = widget.controller.text.trim().isNotEmpty;
    if (has != _hasText) setState(() => _hasText = has);
  }

  /// _send : Hands the text over and clears the field.
  ///
  /// Focus is kept, so the next prompt can be typed straight away — a
  /// conversation is usually more than one question.
  void _send() {
    final text = widget.controller.text.trim();
    if (text.isEmpty || !widget.enabled) return;
    widget.controller.clear();
    widget.onSend(text);
    _focus.requestFocus();
  }

  /// _onKey : Sends on enter, and lets shift-enter through as a newline.
  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    if (event.logicalKey != LogicalKeyboardKey.enter &&
        event.logicalKey != LogicalKeyboardKey.numpadEnter) {
      return KeyEventResult.ignored;
    }
    if (HardwareKeyboard.instance.isShiftPressed) {
      return KeyEventResult.ignored;
    }
    _send();
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final stopping = widget.onStop != null;

    if (widget.listening) return _listeningBar(context);

    return Container(
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border.all(
          color: _focus.hasFocus ? colors.accent : colors.border,
          width: 1.5,
        ),
        borderRadius: BorderRadius.circular(AppRadius.md),
      ),
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.md,
        AppSpacing.sm,
        AppSpacing.sm,
        AppSpacing.sm,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: Focus(
              onKeyEvent: _onKey,
              child: TextField(
                controller: widget.controller,
                focusNode: _focus,
                enabled: widget.enabled,
                autofocus: true,
                maxLines: 6,
                minLines: 1,
                keyboardType: TextInputType.multiline,
                textInputAction: TextInputAction.newline,
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
                    vertical: AppSpacing.sm,
                  ),
                ),
              ),
            ),
          ),
          if (widget.onListen != null) ...[
            const SizedBox(width: AppSpacing.xs),
            _MicButton(listening: false, onTap: widget.onListen!),
          ],
          const SizedBox(width: AppSpacing.sm),
          if (stopping)
            AppButton(
              label: 'Stop',
              icon: Icons.stop_rounded,
              variant: AppButtonVariant.secondary,
              compact: true,
              onPressed: widget.onStop,
            )
          else
            _SendButton(
              enabled: _hasText && widget.enabled,
              busy: widget.busy,
              onTap: _send,
            ),
        ],
      ),
    );
  }
}

/// _SendButton : The round send control.
///
/// Square-ish and icon-only so that it takes no more room than it has to,
/// and dimmed rather than hidden when there is nothing to send, so the
/// composer does not change shape as you type.
class _SendButton extends StatelessWidget {
  const _SendButton({
    required this.enabled,
    required this.busy,
    required this.onTap,
  });

  final bool enabled;
  final bool busy;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Semantics(
      button: true,
      enabled: enabled,
      label: 'Send',
      child: MouseRegion(
        cursor: enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
        child: GestureDetector(
          onTap: enabled && !busy ? onTap : null,
          behavior: HitTestBehavior.opaque,
          child: AnimatedContainer(
            duration: AppMotion.fast,
            width: 32,
            height: 32,
            decoration: BoxDecoration(
              color: enabled ? colors.accent : colors.surfaceRaised,
              borderRadius: BorderRadius.circular(AppRadius.sm),
            ),
            child: Icon(
              Icons.arrow_upward_rounded,
              size: 18,
              color: enabled ? colors.accentText : colors.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}

/// _listeningBar : What the composer becomes while the microphone is open.
///
/// The field is replaced rather than filled, because what is heard is not
/// yet text the user can edit — it is still being revised by the
/// recogniser, and a cursor in it would invite typing that gets overwritten.
extension on _FComposerState {
  Widget _listeningBar(BuildContext context) {
    final colors = context.colors;
    final heard = widget.heard?.trim() ?? '';

    return Container(
      decoration: BoxDecoration(
        color: colors.accentSoft,
        border: Border.all(color: colors.accent, width: 1.5),
        borderRadius: BorderRadius.circular(AppRadius.md),
      ),
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.md,
        AppSpacing.sm,
        AppSpacing.sm,
        AppSpacing.sm,
      ),
      child: Row(
        children: [
          const _ListeningPulse(),
          const SizedBox(width: AppSpacing.md),
          Expanded(
            child: Text(
              heard.isEmpty ? (widget.listeningHint ?? 'Listening…') : heard,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: heard.isEmpty
                  ? context.text.body.copyWith(color: colors.textMuted)
                  : context.text.body,
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          _MicButton(listening: true, onTap: widget.onListen!),
        ],
      ),
    );
  }
}

/// _MicButton : Opens and closes the microphone.
class _MicButton extends StatelessWidget {
  const _MicButton({required this.listening, required this.onTap});

  final bool listening;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Semantics(
      button: true,
      label: listening ? 'Stop listening' : 'Speak',
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: onTap,
          behavior: HitTestBehavior.opaque,
          child: AnimatedContainer(
            duration: AppMotion.fast,
            width: 32,
            height: 32,
            decoration: BoxDecoration(
              color: listening ? colors.accent : Colors.transparent,
              borderRadius: BorderRadius.circular(AppRadius.sm),
            ),
            child: Icon(
              listening ? Icons.stop_rounded : Icons.mic_none_rounded,
              size: 18,
              color: listening ? colors.accentText : colors.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}

/// _ListeningPulse : A dot that breathes while the microphone is open.
///
/// The one unmistakable sign that the assistant is hearing the room, which matters
/// more than it looks: a microphone left open by accident should be
/// obvious.
class _ListeningPulse extends StatefulWidget {
  const _ListeningPulse();

  @override
  State<_ListeningPulse> createState() => _ListeningPulseState();
}

class _ListeningPulseState extends State<_ListeningPulse>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 900),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _controller,
    builder: (context, _) {
      final t = Curves.easeInOut.transform(_controller.value);
      return Container(
        width: 10,
        height: 10,
        decoration: BoxDecoration(
          color: context.colors.accent.withValues(alpha: 0.45 + t * 0.55),
          shape: BoxShape.circle,
        ),
      );
    },
  );
}
