/// Choosing what has to be held to keep listening.
library;

import 'staying_awake_stub.dart'
    if (dart.library.io) 'staying_awake_io.dart'
    as platform;

import 'staying_awake.dart';

/// defaultStayingAwake : What this platform needs held, if anything.
StayingAwake defaultStayingAwake() => platform.createStayingAwake();
