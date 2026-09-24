/// Every component on one screen.
///
/// Kept in the app rather than in a separate storybook so that it cannot
/// drift from what the screens actually use: a component that breaks here
/// breaks there. Reached at `/gallery` in a development build.
library;

import 'package:flutter/material.dart';

import '../friday/friday.dart' show ChatStatus;
import 'design.dart';

/// FGallery : The component catalogue.
class FGallery extends StatefulWidget {
  const FGallery({super.key});

  @override
  State<FGallery> createState() => _FGalleryState();
}

class _FGalleryState extends State<FGallery> {
  final TextEditingController _text = TextEditingController(text: 'dhanush');
  final TextEditingController _password = TextEditingController(text: 'secret');
  final TextEditingController _broken = TextEditingController(text: 'nobody');
  bool _busy = false;
  int _selectedSession = 0;

  @override
  void dispose() {
    _text.dispose();
    _password.dispose();
    _broken.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: context.colors.background,
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: ListView(
              padding: const EdgeInsets.all(FSpacing.xl),
              children: [
                const FWordmark(size: 24),
                const SizedBox(height: FSpacing.xs),
                Text('Design system', style: context.text.caption),
                const SizedBox(height: FSpacing.xxl),

                _Section(
                  'Buttons',
                  children: [
                    Wrap(
                      spacing: FSpacing.sm,
                      runSpacing: FSpacing.sm,
                      children: [
                        FButton(label: 'Send', onPressed: () {}),
                        FButton(
                          label: 'New session',
                          icon: Icons.add,
                          variant: FButtonVariant.secondary,
                          onPressed: () {},
                        ),
                        FButton(
                          label: 'Log out',
                          variant: FButtonVariant.ghost,
                          onPressed: () {},
                        ),
                        FButton(
                          label: 'Revoke',
                          variant: FButtonVariant.danger,
                          onPressed: () {},
                        ),
                        const FButton(label: 'Disabled', onPressed: null),
                        FButton(
                          label: 'Signing in',
                          busy: _busy,
                          onPressed: () {
                            setState(() => _busy = true);
                            Future.delayed(
                              const Duration(seconds: 2),
                              () => mounted
                                  ? setState(() => _busy = false)
                                  : null,
                            );
                          },
                        ),
                      ],
                    ),
                    const SizedBox(height: FSpacing.md),
                    Wrap(
                      spacing: FSpacing.sm,
                      children: [
                        FButton(
                          label: 'Compact',
                          compact: true,
                          onPressed: () {},
                        ),
                        FButton(
                          label: 'Compact ghost',
                          compact: true,
                          variant: FButtonVariant.ghost,
                          onPressed: () {},
                        ),
                      ],
                    ),
                  ],
                ),

                _Section(
                  'Inputs',
                  children: [
                    FTextField(
                      controller: _text,
                      label: 'Username',
                      icon: Icons.person_outline,
                    ),
                    const SizedBox(height: FSpacing.lg),
                    FTextField(
                      controller: _password,
                      label: 'Password',
                      obscure: true,
                      icon: Icons.lock_outline,
                    ),
                    const SizedBox(height: FSpacing.lg),
                    FTextField(
                      controller: _broken,
                      label: 'With an error',
                      error: 'That username or password is not correct.',
                    ),
                  ],
                ),

                _Section(
                  'Messages',
                  children: [
                    FBanner(
                      message: 'I cannot reach FRIDAY at the moment.',
                      actionLabel: 'Try again',
                      onAction: () {},
                    ),
                    const SizedBox(height: FSpacing.md),
                    const FBanner(
                      message: 'This session has been superseded.',
                      tone: FBannerTone.warning,
                    ),
                    const SizedBox(height: FSpacing.md),
                    const FBanner(
                      message: 'Answers are spoken on this device.',
                      tone: FBannerTone.info,
                    ),
                  ],
                ),

                _Section(
                  'Conversation',
                  children: [
                    const FTurn(
                      speaker: FSpeaker.you,
                      text: 'In one short sentence, what is Go?',
                      timestamp: '10:04',
                    ),
                    const FTurn(
                      speaker: FSpeaker.friday,
                      text: 'Let me look into that.',
                      transient: true,
                    ),
                    const FTurn(
                      speaker: FSpeaker.friday,
                      text:
                          'Go is a fast, statically typed, compiled '
                          'programming language created by Google, designed '
                          'for simplicity and efficient concurrent '
                          'programming.',
                      timestamp: '10:04',
                    ),
                    FThinkingTurn(onStop: () {}),
                    const FTurn(
                      speaker: FSpeaker.you,
                      text: 'No, make it four.',
                      superseded: true,
                    ),
                    const FTurn(
                      speaker: FSpeaker.friday,
                      text: 'I could not reach GitLab.',
                      failed: true,
                    ),
                  ],
                ),

                _Section(
                  'Status',
                  children: [
                    Wrap(
                      spacing: FSpacing.lg,
                      runSpacing: FSpacing.sm,
                      children: [
                        for (final status in ChatStatus.values)
                          FStatusDot(status: status),
                      ],
                    ),
                  ],
                ),

                _Section(
                  'Sessions',
                  children: [
                    FSurface(
                      padding: const EdgeInsets.all(FSpacing.sm),
                      child: Column(
                        children: [
                          for (var i = 0; i < 3; i++)
                            FSessionTile(
                              title: const [
                                'Merge requests',
                                'Groceries',
                                'Untitled session',
                              ][i],
                              subtitle: const [
                                '2 minutes ago',
                                'Yesterday',
                                'Last week',
                              ][i],
                              selected: _selectedSession == i,
                              active: i == 0,
                              onTap: () => setState(() => _selectedSession = i),
                            ),
                        ],
                      ),
                    ),
                  ],
                ),

                _Section(
                  'Empty states',
                  children: [
                    FSurface(
                      child: SizedBox(
                        height: 180,
                        child: FEmptyState(
                          icon: Icons.forum_outlined,
                          title: 'Nothing said yet',
                          body:
                              'Ask FRIDAY something and the answer will '
                              'appear here.',
                          action: FButton(
                            label: 'New session',
                            icon: Icons.add,
                            variant: FButtonVariant.secondary,
                            compact: true,
                            onPressed: () {},
                          ),
                        ),
                      ),
                    ),
                  ],
                ),

                _Section(
                  'Spinners',
                  children: [
                    const Row(
                      children: [
                        FSpinner(),
                        SizedBox(width: FSpacing.lg),
                        FSpinner(size: 24),
                        SizedBox(width: FSpacing.lg),
                        FThinkingDots(),
                      ],
                    ),
                  ],
                ),

                const SizedBox(height: FSpacing.xxxl),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// _Section : A labelled group in the gallery.
class _Section extends StatelessWidget {
  const _Section(this.title, {required this.children});

  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(title.toUpperCase(), style: context.text.label),
      const SizedBox(height: FSpacing.md),
      ...children,
      const SizedBox(height: FSpacing.xxl),
    ],
  );
}
