import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/design/design.dart';

/// _nothing : A tap handler for tests that do not care about it.
void _nothing() {}

/// host : One tile, unselected and unhovered, which is the state the menu
/// used to vanish in.
Widget host({
  required VoidCallback onRename,
  required VoidCallback onArchive,
  required VoidCallback onDelete,
  VoidCallback onTap = _nothing,
  bool selected = false,
}) => MaterialApp(
  theme: AppTheme.dark,
  home: Scaffold(
    body: SizedBox(
      width: 300,
      child: AppConversationTile(
        title: 'D minor scale notes',
        subtitle: '16:51',
        selected: selected,
        onTap: onTap,
        onRename: onRename,
        onArchive: onArchive,
        onDelete: onDelete,
      ),
    ),
  ),
);

void main() {
  // The menu button is always in the tree, only invisible. Taking it away
  // unmounts it, and a PopupMenuButton unmounted while its menu is open
  // drops the selection silently: its handler begins "if (!mounted)".
  // Opening the menu ends the hover, which used to take it away, so none
  // of the three actions ever ran.
  testWidgets('the menu button is present even unhovered', (tester) async {
    await tester.pumpWidget(
      host(onRename: () {}, onArchive: () {}, onDelete: () {}),
    );

    expect(find.byIcon(Icons.more_horiz), findsOneWidget);
  });

  for (final label in ['Rename', 'Archive', 'Delete']) {
    testWidgets('$label runs its action', (tester) async {
      var ran = 0, tapped = 0;
      await tester.pumpWidget(
        host(
          onTap: () => tapped++,
          onRename: () => ran++,
          onArchive: () => ran++,
          onDelete: () => ran++,
        ),
      );

      // Hover, as a pointer does, so the menu is reachable.
      final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
      await mouse.addPointer(location: Offset.zero);
      addTearDown(mouse.removePointer);
      await mouse.moveTo(tester.getCenter(find.text('D minor scale notes')));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.more_horiz));
      await tester.pumpAndSettle();
      expect(find.text(label), findsOneWidget, reason: 'the menu did not open');

      // The pointer leaves the tile for the menu, which is what used to
      // unmount the button and lose the selection.
      await mouse.moveTo(const Offset(5, 5));
      await tester.pump();

      await tester.tap(find.text(label));
      await tester.pumpAndSettle();

      expect(ran, 1, reason: '$label did nothing');
      expect(
        tapped,
        0,
        reason: 'the tap fell through and switched conversation',
      );
    });
  }

  // An archived one offers to bring it back, since the same control has
  // to say which way it goes.
  testWidgets('an archived conversation offers Unarchive', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.dark,
        home: Scaffold(
          body: SizedBox(
            width: 300,
            child: AppConversationTile(
              title: 'Old',
              selected: false,
              onTap: _nothing,
              archived: true,
              onArchive: () {},
            ),
          ),
        ),
      ),
    );

    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
    await mouse.addPointer(location: Offset.zero);
    addTearDown(mouse.removePointer);
    await mouse.moveTo(tester.getCenter(find.text('Old')));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_horiz));
    await tester.pumpAndSettle();

    expect(find.text('Unarchive'), findsOneWidget);
    expect(find.text('Archive'), findsNothing);
  });

  // A tile with nothing to offer shows no control at all.
  testWidgets('no actions means no menu', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.dark,
        home: Scaffold(
          body: SizedBox(
            width: 300,
            child: AppConversationTile(
              title: 'Bare',
              selected: false,
              onTap: _nothing,
            ),
          ),
        ),
      ),
    );

    expect(find.byIcon(Icons.more_horiz), findsNothing);
  });
}
