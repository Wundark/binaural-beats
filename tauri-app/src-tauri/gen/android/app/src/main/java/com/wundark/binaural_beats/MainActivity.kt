package com.wundark.binaural_beats

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat

class MainActivity : TauriActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)
  }

  override fun onWebViewCreate(webView: WebView) {
    super.onWebViewCreate(webView)
    webView.addJavascriptInterface(PlaybackBridge(), "AndroidPlayback")
  }

  /** Lets the page keep playback running in the background (see PlaybackService). */
  inner class PlaybackBridge {
    private var askedForNotifications = false

    @JavascriptInterface
    fun update(title: String, playing: Boolean, remainingMs: Double) {
      if (playing) askForNotificationPermission()
      PlaybackService.update(this@MainActivity, title, playing, remainingMs.toLong())
    }

    @JavascriptInterface
    fun stop() {
      PlaybackService.stop(this@MainActivity)
    }

    // Android 13+ hides the playback notification without this permission;
    // playback still works either way.
    private fun askForNotificationPermission() {
      if (askedForNotifications || Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
      askedForNotifications = true
      val permission = Manifest.permission.POST_NOTIFICATIONS
      if (ContextCompat.checkSelfPermission(this@MainActivity, permission) != PackageManager.PERMISSION_GRANTED) {
        runOnUiThread { ActivityCompat.requestPermissions(this@MainActivity, arrayOf(permission), 1) }
      }
    }
  }

  override fun onDestroy() {
    // The session lives in this process; don't leave the notification behind.
    if (isFinishing) PlaybackService.stop(this)
    super.onDestroy()
  }
}
