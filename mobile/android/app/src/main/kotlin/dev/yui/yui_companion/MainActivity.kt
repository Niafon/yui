package dev.yui.yui_companion

import android.app.Activity
import android.content.Intent
import android.graphics.Bitmap
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import androidx.core.content.ContextCompat
import dev.yui.companion.AudioBridge
import dev.yui.companion.VoiceSessionService
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream

class MainActivity : FlutterActivity() {
    private val mainHandler = Handler(Looper.getMainLooper())
    private var cameraResult: MethodChannel.Result? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "yui/audio").setMethodCallHandler { call, result ->
            when (call.method) {
                "start" -> {
                    try {
                        ContextCompat.startForegroundService(this, Intent(this, VoiceSessionService::class.java))
                        result.success(null)
                    } catch (error: Exception) {
                        result.error("audio_start", error.message, null)
                    }
                }
                "stop" -> {
                    stopService(Intent(this, VoiceSessionService::class.java))
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "yui/audio_chunks")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
                    AudioBridge.sink = { chunk -> mainHandler.post { events.success(chunk) } }
                }

                override fun onCancel(arguments: Any?) {
                    AudioBridge.sink = null
                }
            })
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "yui/camera").setMethodCallHandler { call, result ->
            if (call.method != "capture") {
                result.notImplemented()
            } else if (cameraResult != null) {
                result.error("camera_busy", "Камера уже открыта", null)
            } else {
                try {
                    cameraResult = result
                    startActivityForResult(Intent(MediaStore.ACTION_IMAGE_CAPTURE), CAMERA_REQUEST)
                } catch (error: Exception) {
                    cameraResult = null
                    result.error("camera_start", error.message, null)
                }
            }
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != CAMERA_REQUEST) return
        val pending = cameraResult ?: return
        cameraResult = null
        if (resultCode != Activity.RESULT_OK) {
            pending.success(null)
            return
        }
        val bitmap = data?.extras?.get("data") as? Bitmap
        if (bitmap == null) {
            pending.error("camera_empty", "Камера не вернула фото", null)
            return
        }
        val output = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.JPEG, 90, output)
        pending.success(output.toByteArray())
    }

    override fun onDestroy() {
        cameraResult?.error("camera_closed", "Камера закрыта", null)
        cameraResult = null
        AudioBridge.sink = null
        stopService(Intent(this, VoiceSessionService::class.java))
        super.onDestroy()
    }

    companion object {
        private const val CAMERA_REQUEST = 4202
    }
}
