import 'dart:convert';

import 'package:http/http.dart' as http;

/// REST client for Yui Core.
///
/// The phone is a sensor and a speaker, never a source of truth (ADR-002,
/// CORE-008): it reads and streams, but the PC owns the data.
class CoreClient {
  CoreClient({required this.baseUrl, required this.token});

  final String baseUrl;
  final String token;

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer $token',
      };

  Future<Map<String, dynamic>> _post(String path, Map<String, dynamic> body) async {
    final response = await http.post(
      Uri.parse('$baseUrl$path'),
      headers: _headers,
      body: jsonEncode(body),
    );
    if (response.statusCode >= 300) {
      throw CoreException(response.statusCode, response.body);
    }
    return jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> _patch(String path, Map<String, dynamic> body) async {
    final response = await http.patch(
      Uri.parse('$baseUrl$path'),
      headers: _headers,
      body: jsonEncode(body),
    );
    if (response.statusCode >= 300) {
      throw CoreException(response.statusCode, response.body);
    }
    return jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> _get(String path) async {
    final response = await http.get(Uri.parse('$baseUrl$path'), headers: _headers);
    if (response.statusCode >= 300) {
      throw CoreException(response.statusCode, response.body);
    }
    final decoded = jsonDecode(utf8.decode(response.bodyBytes));
    return decoded is Map<String, dynamic> ? decoded : {'items': decoded};
  }

  Future<Map<String, dynamic>> status() => _get('/v1/status');

  Future<Map<String, dynamic>> inferenceStatus() => _get('/v1/inference');

  Future<Map<String, dynamic>> updateInferencePreferences(Map<String, dynamic> patch) =>
      _patch('/v1/inference/preferences', patch);

  Future<Map<String, dynamic>> startSession() => _post('/v1/sessions', {'mode': 'normal'});

  Future<Map<String, dynamic>> sendVisionFrame(String sessionId, String imageB64, String question) =>
      _post('/v1/sessions/$sessionId/vision', {
        'image_b64': imageB64,
        'mime': 'image/jpeg',
        'question': question,
      });

  Future<void> setOutputDevice(String sessionId, String deviceId) =>
      _post('/v1/sessions/$sessionId/output-device', {'device_id': deviceId});

  Future<void> resolvePermission(String pendingId, {required bool allow, required bool remember}) =>
      _post('/v1/permissions/pending/$pendingId', {'allow': allow, 'remember': remember});
}

class CoreException implements Exception {
  CoreException(this.status, this.body);

  final int status;
  final String body;

  @override
  String toString() => 'Ядро ответило $status: $body';
}
