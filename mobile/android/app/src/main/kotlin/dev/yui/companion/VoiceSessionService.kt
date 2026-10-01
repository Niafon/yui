package dev.yui.companion

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import kotlin.concurrent.thread

/**
 * Foreground service for the voice session (CL-002).
 *
 * Android stops background microphone access aggressively, so a long session
 * must be a foreground service with a persistent, honest notification: the
 * owner should always be able to see that the microphone is live (CL-003).
 */
class VoiceSessionService : Service() {

    private var record: AudioRecord? = null
    @Volatile private var running = false

    override fun onCreate() {
        super.onCreate()
        createChannel()
        startForeground(NOTIFICATION_ID, buildNotification("Микрофон активен"))
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (!running) startCapture()
        // Restart if the system kills us mid conversation.
        return START_STICKY
    }

    private fun startCapture() {
        val bufferSize = AudioRecord.getMinBufferSize(
            SAMPLE_RATE,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        ).coerceAtLeast(CHUNK_BYTES)

        val recorder = AudioRecord(
            MediaRecorder.AudioSource.VOICE_COMMUNICATION,
            SAMPLE_RATE,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
            bufferSize,
        )
        record = recorder
        running = true
        recorder.startRecording()

        thread(name = "yui-audio") {
            val buffer = ByteArray(CHUNK_BYTES)
            while (running) {
                val read = recorder.read(buffer, 0, buffer.size)
                if (read > 0) {
                    // Handed to the Dart side over an event channel, which
                    // streams it to the core on the data plane.
                    AudioBridge.emit(buffer.copyOf(read))
                }
            }
        }
    }

    override fun onDestroy() {
        running = false
        record?.stop()
        record?.release()
        record = null
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private fun buildNotification(text: String): Notification =
        NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Yui слушает")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_btn_speak_now)
            .setOngoing(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .build()

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "Голосовая сессия",
                NotificationManager.IMPORTANCE_LOW,
            )
            getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
        }
    }

    companion object {
        const val CHANNEL_ID = "yui.voice"
        const val NOTIFICATION_ID = 4201
        const val SAMPLE_RATE = 16000
        const val CHUNK_BYTES = 3200 // 100 ms of 16 kHz mono PCM16
    }
}

/** Bridge between the capture thread and the Flutter event channel. */
object AudioBridge {
    @Volatile var sink: ((ByteArray) -> Unit)? = null

    fun emit(chunk: ByteArray) {
        sink?.invoke(chunk)
    }
}
