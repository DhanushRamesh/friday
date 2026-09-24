/// The byte source for a platform that has neither dart:io nor a browser.
library;

import 'byte_source.dart';

/// createByteSource : Always throws. Reaching this means the conditional
/// import in byte_source.dart matched no real implementation, which is a
/// build problem rather than something a caller can handle.
ByteSource createByteSource() =>
    throw UnsupportedError('FRIDAY has no way to stream on this platform.');
