package com.wundark.binaural_beats

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.view.View
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class MainActivity : TauriActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    // The page is always dark, so the system bars keep light icons.
    enableEdgeToEdge(
      statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
      navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT)
    )
    super.onCreate(savedInstanceState)
    // Edge to edge, the web view would draw under the status and navigation
    // bars. Pad it clear of them (and of any display cutout); the window
    // background, the page colour, shows behind the bars.
    val content = findViewById<View>(android.R.id.content)
    ViewCompat.setOnApplyWindowInsetsListener(content) { view, insets ->
      val bars = insets.getInsets(
        WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
      )
      view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
      WindowInsetsCompat.CONSUMED
    }
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
