package com.chilltongx.landrop.samsung.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val LightColors = lightColorScheme(
    primary = Color(0xFF087EA4),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFD5F3FF),
    onPrimaryContainer = Color(0xFF003544),
    secondary = Color(0xFF4D626A),
    background = Color(0xFFF7F8FA),
    surface = Color(0xFFF7F8FA),
    surfaceContainer = Color.White,
    outlineVariant = Color(0xFFDDE3E7),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFF7BD4F5),
    onPrimary = Color(0xFF003544),
    primaryContainer = Color(0xFF005066),
    onPrimaryContainer = Color(0xFFC2EFFF),
    background = Color(0xFF0F1115),
    surface = Color(0xFF0F1115),
    surfaceContainer = Color(0xFF191C21),
    outlineVariant = Color(0xFF3E464C),
)

@Composable
fun LanDropTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val context = LocalContext.current
    val colors = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
    } else if (dark) {
        DarkColors
    } else {
        LightColors
    }
    MaterialTheme(colorScheme = colors, content = content)
}
