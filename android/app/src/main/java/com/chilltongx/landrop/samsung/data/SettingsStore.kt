package com.chilltongx.landrop.samsung.data

import android.content.Context
import androidx.core.content.edit

class SettingsStore(context: Context) {
    private val preferences = context.getSharedPreferences("lan_drop", Context.MODE_PRIVATE)

    init {
        preferences.edit { remove("token") }
    }

    fun load(): SavedConnection = SavedConnection(
        serverUrl = preferences.getString("server_url", "").orEmpty(),
    )

    fun save(serverUrl: String) {
        preferences.edit {
            putString("server_url", serverUrl)
        }
    }
}

data class SavedConnection(
    val serverUrl: String,
)
