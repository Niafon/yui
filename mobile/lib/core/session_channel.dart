import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

/// One frame from the core, on either plane.
class Frame {
  Frame(this.type, this.payload);

  final String type;
  final Map<String, dynamic> payload;

  static Frame? decode(dynamic raw) {
    try {
      final data = jsonDecode(raw as String) as Map<String, dynamic>;
      return Frame(
        data['type'] as String? ?? '',
        (data['payload'] as Map<String, dynamic>?) ?? const {},
      );
    } on FormatException {
      return null;
    }
  }
}

/// Control and data planes are separate sockets, mirroring the core (SRS 5.2).
class SessionChannel {
  SessionChannel(
      {required this.baseUrl, required this.token, required this.sessionId});

  final String baseUrl;
  final String token;
  final String sessionId;

  WebSocketChannel? _control;
  WebSocketChannel? _data;
  final _frames = StreamController<Frame>.broadcast();
  bool _disposed = false;

  Stream<Frame> get frames => _frames.stream;

  String _url(String plane) {
    final ws = baseUrl.replaceFirst(RegExp('^http'), 'ws');
    return '$ws/v1/$plane?session=$sessionId&token=$token';
  }

  Future<void> connect() async {
    _control = WebSocketChannel.connect(Uri.parse(_url('control')));
    _data = WebSocketChannel.connect(Uri.parse(_url('data')));
    for (final channel in [_control!, _data!]) {
      channel.stream.listen(
        (raw) {
          final frame = Frame.decode(raw);
          if (!_disposed && frame != null) _frames.add(frame);
        },
        onError: (Object error) {
          if (!_disposed) _frames.addError(error);
        },
        onDone: () {
          if (!_disposed) _frames.add(Frame('connection.closed', const {}));
        },
      );
    }
    try {
      await Future.wait([_control!.ready, _data!.ready]);
    } catch (_) {
      await dispose();
      rethrow;
    }
  }

  /// Streams captured PCM to the core. Audio is sent as binary on the data
  /// plane so it never blocks control traffic.
  void sendAudio(List<int> pcm) => _data?.sink.add(pcm);

  void endUtterance({int sampleRate = 16000, int channels = 1}) => _send({
        'type': 'audio.end',
        'sample_rate': sampleRate,
        'channels': channels,
      });

  void clearAudio() => _send({'type': 'audio.clear'});

  void sendText(String text) => _send({'type': 'text', 'text': text});

  void setMicrophone({required bool muted}) =>
      _send({'type': 'mic.state', 'text': muted ? 'muted' : 'live'});

  void cancel() => _send({'type': 'session.cancel'});

  void _send(Map<String, dynamic> message) =>
      _data?.sink.add(jsonEncode(message));

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await _control?.sink.close();
    await _data?.sink.close();
    await _frames.close();
  }
}
