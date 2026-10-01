import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

/// Pairing state: the one-time code from the PC is exchanged for a device
/// token, which is stored locally and can be revoked from the PC (CL-004,
/// SEC-010).
class Pairing {
  Pairing({required this.baseUrl, required this.token, required this.deviceId});

  final String baseUrl;
  final String token;
  final String deviceId;

  static const _keyBase = 'yui.base_url';
  static const _keyToken = 'yui.token';
  static const _keyDevice = 'yui.device_id';

  /// Parses the QR payload produced by the desktop stage.
  static ({String baseUrl, String code})? parseQr(String raw) {
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final host = data['host'] as String?;
      final code = data['code'] as String?;
      if (host == null || code == null) return null;
      final base = normalizeBaseUrl(host);
      return (baseUrl: base, code: code);
    } catch (_) {
      return null;
    }
  }

  static String normalizeBaseUrl(String raw) {
    final value = raw.trim();
    if (value.isEmpty) throw const FormatException('Укажите адрес компьютера');
    final uri = Uri.tryParse(value.contains('://') ? value : 'https://$value');
    if (uri == null ||
        uri.host.isEmpty ||
        (uri.scheme != 'https' && uri.scheme != 'http')) {
      throw const FormatException('Неверный адрес компьютера');
    }
    final loopback = {'localhost', '127.0.0.1', '::1'}.contains(uri.host);
    if (uri.scheme == 'http' && !loopback) {
      throw const FormatException('Для подключения телефона нужен HTTPS');
    }
    if (uri.host == '0.0.0.0' ||
        uri.host == '::' ||
        uri.userInfo.isNotEmpty ||
        (uri.path.isNotEmpty && uri.path != '/') ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException(
          'Укажите доступный адрес компьютера без пути');
    }
    return uri.origin;
  }

  static Future<Pairing> claim({
    required String baseUrl,
    required String code,
    required String deviceName,
  }) async {
    baseUrl = normalizeBaseUrl(baseUrl);
    final response = await http.post(
      Uri.parse('$baseUrl/v1/pair/claim'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'code': code,
        'name': deviceName,
        'kind': 'android',
        'capabilities': [
          'audio.capture',
          'audio.playback',
          'vision.capture',
          'notifications.read'
        ],
      }),
    );
    if (response.statusCode >= 300) {
      throw StateError('Сопряжение отклонено: ${response.body}');
    }
    final body =
        jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final pairing = Pairing(
      baseUrl: baseUrl,
      token: body['token'] as String,
      deviceId: body['device_id'] as String,
    );
    await pairing.save();
    return pairing;
  }

  Future<void> save() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyBase, baseUrl);
    await prefs.setString(_keyToken, token);
    await prefs.setString(_keyDevice, deviceId);
  }

  static Future<Pairing?> load() async {
    final prefs = await SharedPreferences.getInstance();
    final base = prefs.getString(_keyBase);
    final token = prefs.getString(_keyToken);
    final device = prefs.getString(_keyDevice);
    if (base == null || token == null || device == null) return null;
    try {
      return Pairing(
        baseUrl: normalizeBaseUrl(base),
        token: token,
        deviceId: device,
      );
    } on FormatException {
      // A pre-TLS remote URL must not be reused after upgrading the client.
      await forget();
      return null;
    }
  }

  static Future<void> forget() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_keyBase);
    await prefs.remove(_keyToken);
    await prefs.remove(_keyDevice);
  }
}
