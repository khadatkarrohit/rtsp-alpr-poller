// ─────────────────────────────────────────
// ALPR Poller — Dart
// Camera: Prama PT-NC140D3-WNM(D2)
// ─────────────────────────────────────────
// Install: https://dart.dev/get-dart
// Run:     dart run poller.dart

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

// ── Config (edit these)
const rtspUrl    = 'rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101';
const apiKey     = 'your key, if any';
const apiBase    = 'your api url';
const country    = 'IND';
const intervalMs = 2000;

const currFrame = '/tmp/alpr_curr.jpg';
const prevFrame = '/tmp/alpr_prev.jpg';

// ── Capture one JPEG frame via ffmpeg
Future<({Uint8List current, Uint8List? previous})> captureFrame() async {
  final curr = File(currFrame);
  final prev = File(prevFrame);

  if (await curr.exists()) {
    await curr.copy(prevFrame);
  }

  final result = await Process.run('ffmpeg', [
    '-loglevel', 'error',
    '-rtsp_transport', 'tcp',
    '-i', rtspUrl,
    '-frames:v', '1',
    '-q:v', '3',
    '-vf', 'scale=640:480',
    currFrame, '-y',
  ]);

  if (result.exitCode != 0) {
    throw Exception('ffmpeg exited ${result.exitCode}');
  }

  final current  = await curr.readAsBytes();
  final previous = await prev.exists() ? await prev.readAsBytes() : null;
  return (current: current, previous: previous);
}

// ── Motion detection: simple byte diff score
bool detectMotion(Uint8List current, Uint8List? previous) {
  if (previous == null) return false;

  final len = current.length < previous.length ? current.length : previous.length;
  var diff = 0.0;
  var samples = 0;
  for (var i = 0; i < len; i += 10) {
    diff += (current[i] - previous[i]).abs();
    samples++;
  }
  final score = (diff / samples) / 255.0 * 100.0;
  print('[motion] ${score.toStringAsFixed(2)}%');
  return score > 30;
}

// ── POST frame to ALPR API
Future<Map<String, dynamic>> scanPlate() async {
  final boundary = 'dartboundary${DateTime.now().millisecondsSinceEpoch}';
  final imageBytes = await File(currFrame).readAsBytes();

  final body = <int>[];
  void writeln(String s) => body.addAll(utf8.encode('$s\r\n'));

  writeln('--$boundary');
  writeln('Content-Disposition: form-data; name="image"; filename="frame.jpg"');
  writeln('Content-Type: image/jpeg');
  writeln('');
  body.addAll(imageBytes);
  writeln('');
  writeln('--$boundary');
  writeln('Content-Disposition: form-data; name="country"');
  writeln('');
  writeln(country);
  body.addAll(utf8.encode('--$boundary--\r\n'));

  final client = HttpClient();
  final uri    = Uri.parse('$apiBase/plate/recognize');
  final req    = await client.postUrl(uri);

  req.headers.set('x-api-key', apiKey);
  req.headers.set('Content-Type', 'multipart/form-data; boundary=$boundary');
  req.add(body);

  final res     = await req.close();
  final payload = await utf8.decodeStream(res);
  client.close();

  if (res.statusCode >= 300) throw Exception('ALPR API ${res.statusCode}');
  return jsonDecode(payload) as Map<String, dynamic>;
}

// ── Notify backend after confirmed plate
Future<void> notifyBackend(Map<String, dynamic> result) async {
  final client = HttpClient();
  final uri    = Uri.parse(apiBase.replaceAll('/api/v1', '/api/entry'));
  final req    = await client.postUrl(uri);

  req.headers.contentType = ContentType.json;
  req.headers.set('x-api-key', apiKey);
  req.write(jsonEncode({
    'plate':      result['plate'],
    'confidence': result['confidence'],
    'country':    country,
    'timestamp':  DateTime.now().toUtc().toIso8601String(),
    'source':     'dart-poller-v1',
  }));

  await (await req.close()).drain<void>();
  client.close();
}

// ── Main loop
Future<void> main() async {
  print('[alpr] dart poller started');
  print('[camera] $rtspUrl');

  var lastPlateTime     = DateTime.fromMillisecondsSinceEpoch(0);
  var consecutiveErrors = 0;
  final interval        = Duration(milliseconds: intervalMs);
  final cooldown        = const Duration(seconds: 8);

  while (true) {
    final t0 = DateTime.now();

    try {
      if (DateTime.now().difference(lastPlateTime) < cooldown) {
        print('[cooldown] skipping');
        await Future.delayed(interval);
        continue;
      }

      final (:current, :previous) = await captureFrame();
      consecutiveErrors = 0;

      if (!detectMotion(current, previous)) {
        print('[motion] none');
      } else {
        print('[motion] detected — scanning plate');
        final result = await scanPlate();
        print('[alpr] $result');

        final plate = result['plate'];
        if (plate != null && plate.toString().isNotEmpty) {
          print('[plate] $plate (${result['confidence']}%)');
          await notifyBackend(result);
          lastPlateTime = DateTime.now();
          print('[cooldown] 8s');
        } else {
          print('[plate] none found');
        }
      }
    } catch (e) {
      consecutiveErrors++;
      print('[error] #$consecutiveErrors: $e');
      if (consecutiveErrors >= 3) {
        print('[error] too many failures, waiting 30s');
        await Future.delayed(const Duration(seconds: 30));
        consecutiveErrors = 0;
      }
    }

    final elapsed = DateTime.now().difference(t0);
    if (elapsed < interval) await Future.delayed(interval - elapsed);
  }
}
