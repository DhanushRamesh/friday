/// Listening where there is no known way to.
library;

import 'listener.dart';

/// createEars : Ears that hear nothing, rather than a build failure.
Ears createEars({String language = 'en-US'}) => DeafEars();
