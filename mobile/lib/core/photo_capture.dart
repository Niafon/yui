import 'package:flutter/services.dart';

/// Opens the Android camera app and receives its JPEG preview image.
class PhotoCapture {
  static const _channel = MethodChannel('yui/camera');

  Future<Uint8List?> capture() => _channel.invokeMethod<Uint8List>('capture');
}
