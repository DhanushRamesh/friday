package com.dhanushramesh.friday_client

import android.content.Context
import android.media.AudioManager
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * MainActivity : The app's only activity, plus the one thing Flutter cannot
 * reach on its own.
 *
 * Android's speech recogniser plays a tone every time a session starts and
 * another when it ends, and offers no way to turn them off. Always awake
 * that is a session per question and another every time the engine times out
 * on an empty room, so the phone clicks at nothing all day.
 *
 * The tones are played through the ordinary audio streams, so silencing them
 * means silencing those streams for the moment the session begins. That is a
 * blunt instrument and it is the only one there is.
 */
class MainActivity : FlutterActivity() {
    /** channel : Named for what it does rather than for the plugin it is not. */
    private val channel = "friday/quiet"

    /**
     * muted : Which streams this has silenced, so exactly those are restored.
     *
     * Kept rather than assumed: restoring a stream this did not mute would
     * unmute something the user silenced themselves.
     */
    private val muted = mutableSetOf<Int>()

    /**
     * streams : Where recognisers have been observed to play their tones.
     *
     * Which one it is depends on the manufacturer, so all three are covered.
     * Music is included because that is where most builds play it, and it is
     * also why this is held for as short a time as possible.
     */
    private val streams = listOf(
        AudioManager.STREAM_MUSIC,
        AudioManager.STREAM_SYSTEM,
        AudioManager.STREAM_NOTIFICATION,
    )

    override fun configureFlutterEngine(engine: FlutterEngine) {
        super.configureFlutterEngine(engine)

        MethodChannel(engine.dartExecutor.binaryMessenger, channel)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "mute" -> { setMuted(true); result.success(null) }
                    "unmute" -> { setMuted(false); result.success(null) }
                    else -> result.notImplemented()
                }
            }
    }

    /** setMuted : Silences or restores the streams a tone might come out of. */
    private fun setMuted(quiet: Boolean) {
        val audio = getSystemService(Context.AUDIO_SERVICE) as? AudioManager ?: return

        if (quiet) {
            for (stream in streams) {
                // Already silent by the user's own choice: leave it alone, and
                // do not remember it as something to turn back on.
                if (audio.getStreamVolume(stream) == 0) continue
                audio.adjustStreamVolume(stream, AudioManager.ADJUST_MUTE, 0)
                muted.add(stream)
            }
            return
        }

        for (stream in muted) {
            audio.adjustStreamVolume(stream, AudioManager.ADJUST_UNMUTE, 0)
        }
        muted.clear()
    }

    /**
     * Restoring on the way out matters more than it looks: a crash or a
     * swipe-away while muted would leave the phone silent with nothing on
     * screen to explain why.
     */
    override fun onDestroy() {
        setMuted(false)
        super.onDestroy()
    }

    override fun onPause() {
        setMuted(false)
        super.onPause()
    }
}
