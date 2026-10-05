package com.wundark.binaural_beats

import org.json.JSONObject

/**
 * Direct calls into the audio engine (see the JNI export in lib.rs), for the
 * playback service, which runs while the page may be suspended.
 */
object Engine {
    init {
        System.loadLibrary("binaural_beats_lib")
    }

    @JvmStatic
    private external fun rpc(request: String): String

    private var nextId = 1

    /** Calls [method], returning its result, or null if it failed. */
    @Synchronized
    fun call(method: String, params: JSONObject? = null): Any? {
        val request = JSONObject()
            .put("jsonrpc", "2.0")
            .put("method", method)
            .put("id", nextId++)
        if (params != null) request.put("params", params)
        return try {
            val response = JSONObject(rpc(request.toString()))
            if (response.has("error")) {
                Logger.error("Engine $method: ${response.optJSONObject("error")?.optString("message")}")
                null
            } else {
                response.opt("result")
            }
        } catch (e: Exception) {
            Logger.error("Engine $method: $e")
            null
        }
    }

    fun status(): Status? = (call("get_status") as? JSONObject)?.let(::Status)

    class Status(json: JSONObject) {
        val playing = json.optBoolean("is_playing")
        val paused = json.optBoolean("is_paused")
        val name: String = json.optString("name")
        val timeMs = (json.optDouble("time", 0.0) * 1000).toLong()
        val durationMs = (json.optDouble("total_duration", 0.0) * 1000).toLong()
        val playlistIndex = json.optInt("playlist_index", -1)
        val playlistLength = json.optInt("playlist_length", 0)
    }
}
